package app

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands"
	"github.com/jesseduffield/lazygit/pkg/config"
	"github.com/jesseduffield/lazygit/pkg/gui"
	"github.com/jesseduffield/lazygit/pkg/i18n"
	"github.com/jesseduffield/lazygit/pkg/updates"
	"github.com/shibukawa/configdir"
	"github.com/sirupsen/logrus"
)

// App struct
type App struct {
	closers []io.Closer

	Config        config.AppConfigurer
	Log           *logrus.Entry
	OSCommand     *commands.OSCommand
	GitCommand    *commands.GitCommand
	Gui           *gui.Gui
	Tr            *i18n.Localizer
	Updater       *updates.Updater // may only need this on the Gui
	ClientContext string
}

type errorMapping struct {
	originalError string
	newError      string
}

func newProductionLogger(config config.AppConfigurer) *logrus.Logger {
	log := logrus.New()
	log.Out = ioutil.Discard
	log.SetLevel(logrus.ErrorLevel)
	return log
}

func globalConfigDir() string {
	configDirs := configdir.New("jesseduffield", "lazygit")
	configDir := configDirs.QueryFolders(configdir.Global)[0]
	return configDir.Path
}

func getLogLevel() logrus.Level {
	strLevel := os.Getenv("LOG_LEVEL")
	level, err := logrus.ParseLevel(strLevel)
	if err != nil {
		return logrus.DebugLevel
	}
	return level
}

func newDevelopmentLogger(config config.AppConfigurer) *logrus.Logger {
	log := logrus.New()
	log.SetLevel(getLogLevel())
	file, err := os.OpenFile(filepath.Join(globalConfigDir(), "development.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("unable to log to file") // TODO: don't panic (also, remove this call to the `panic` function)
	}
	log.SetOutput(file)
	return log
}

func newLogger(config config.AppConfigurer) *logrus.Entry {
	var log *logrus.Logger
	if config.GetDebug() || os.Getenv("DEBUG") == "TRUE" {
		log = newDevelopmentLogger(config)
	} else {
		log = newProductionLogger(config)
	}

	// highly recommended: tail -f development.log | humanlog
	// https://github.com/aybabtme/humanlog
	log.Formatter = &logrus.JSONFormatter{}

	return log.WithFields(logrus.Fields{
		"debug":     config.GetDebug(),
		"version":   config.GetVersion(),
		"commit":    config.GetCommit(),
		"buildDate": config.GetBuildDate(),
	})
}

// NewApp bootstrap a new application
func NewApp(config config.AppConfigurer, filterPath string) (*App, error) {
	app := &App{
		closers: []io.Closer{},
		Config:  config,
	}
	var err error
	app.Log = newLogger(config)
	app.Tr = i18n.NewLocalizer(app.Log)

	// if we are being called in 'demon' mode, we can just return here
	app.ClientContext = os.Getenv("LAZYGIT_CLIENT_COMMAND")
	if app.ClientContext != "" {
		return app, nil
	}

	app.OSCommand = commands.NewOSCommand(app.Log, config)

	app.Updater, err = updates.NewUpdater(app.Log, config, app.OSCommand, app.Tr)
	if err != nil {
		return app, err
	}

	if err := app.setupRepo(); err != nil {
		return app, err
	}

	app.GitCommand, err = commands.NewGitCommand(app.Log, app.OSCommand, app.Tr, app.Config)
	if err != nil {
		return app, err
	}
	app.Gui, err = gui.NewGui(app.Log, app.GitCommand, app.OSCommand, app.Tr, config, app.Updater, filterPath)
	if err != nil {
		return app, err
	}
	return app, nil
}

func (app *App) setupRepo() error {
	// if we are not in a git repo, we ask if we want to `git init`
	if err := app.OSCommand.RunCommand("git status"); err != nil {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		info, _ := os.Stat(filepath.Join(cwd, ".git"))
		if info != nil && info.IsDir() {
			return err // Current directory appears to be a git repository.
		}

		// Offer to initialize a new repository in current directory.
		fmt.Print(app.Tr.SLocalize("CreateRepo"))
		response, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.Trim(response, " \n") != "y" {
			os.Exit(1)
		}
		if err := app.OSCommand.RunCommand("git init"); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) Run() error {
	if app.ClientContext == "INTERACTIVE_REBASE" {
		return app.Rebase()
	}

	if app.ClientContext == "EXIT_IMMEDIATELY" {
		os.Exit(0)
	}

	err := app.Gui.RunWithSubprocesses()
	return err
}

// Rebase contains logic for when we've been run in demon mode, meaning we've
// given lazygit as a command for git to call e.g. to edit a file
func (app *App) Rebase() error {
	app.Log.Info("Lazygit invoked as interactive rebase demon")
	app.Log.Info("args: ", os.Args)

	if strings.HasSuffix(os.Args[1], "git-rebase-todo") {
		if err := ioutil.WriteFile(os.Args[1], []byte(os.Getenv("LAZYGIT_REBASE_TODO")), 0644); err != nil {
			return err
		}

	} else if strings.HasSuffix(os.Args[1], ".git/COMMIT_EDITMSG") {
		// if we are rebasing and squashing, we'll see a COMMIT_EDITMSG
		// but in this case we don't need to edit it, so we'll just return
	} else {
		app.Log.Info("Lazygit demon did not match on any use cases")
	}

	return nil
}

// Close closes any resources
func (app *App) Close() error {
	for _, closer := range app.closers {
		err := closer.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// KnownError takes an error and tells us whether it's an error that we know about where we can print a nicely formatted version of it rather than panicking with a stack trace
func (app *App) KnownError(err error) (string, bool) {
	errorMessage := err.Error()

	mappings := []errorMapping{
		{
			originalError: "fatal: not a git repository (or any of the parent directories): .git",
			newError:      app.Tr.SLocalize("notARepository"),
		},
	}

	for _, mapping := range mappings {
		if strings.Contains(errorMessage, mapping.originalError) {
			return mapping.newError, true
		}
	}
	return "", false
}
