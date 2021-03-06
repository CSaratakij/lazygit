package gui

import (
	"strings"

	"github.com/jesseduffield/gocui"
)

type credentials chan string

// waitForPassUname wait for a username or password input from the credentials popup
func (gui *Gui) waitForPassUname(g *gocui.Gui, currentView *gocui.View, passOrUname string) string {
	gui.credentials = make(chan string)
	g.Update(func(g *gocui.Gui) error {
		credentialsView, _ := g.View("credentials")
		if passOrUname == "username" {
			credentialsView.Title = gui.Tr.SLocalize("CredentialsUsername")
			credentialsView.Mask = 0
		} else {
			credentialsView.Title = gui.Tr.SLocalize("CredentialsPassword")
			credentialsView.Mask = '*'
		}
		err := gui.switchFocus(g, currentView, credentialsView)
		if err != nil {
			return err
		}
		gui.RenderCommitLength()
		return nil
	})

	// wait for username/passwords input
	userInput := <-gui.credentials
	return userInput + "\n"
}

func (gui *Gui) handleSubmitCredential(g *gocui.Gui, v *gocui.View) error {
	message := gui.trimmedContent(v)
	gui.credentials <- message
	v.Clear()
	_ = v.SetCursor(0, 0)
	_, _ = g.SetViewOnBottom("credentials")
	nextView, err := gui.g.View("confirmation")
	if err != nil {
		nextView = gui.getFilesView()
	}
	err = gui.switchFocus(g, nil, nextView)
	if err != nil {
		return err
	}
	return gui.refreshSidePanels(refreshOptions{})
}

func (gui *Gui) handleCloseCredentialsView(g *gocui.Gui, v *gocui.View) error {
	_, err := g.SetViewOnBottom("credentials")
	if err != nil {
		return err
	}

	gui.credentials <- ""
	return gui.switchFocus(g, nil, gui.getFilesView())
}

func (gui *Gui) handleCredentialsViewFocused(g *gocui.Gui, v *gocui.View) error {
	if _, err := g.SetViewOnTop("credentials"); err != nil {
		return err
	}

	message := gui.Tr.TemplateLocalize(
		"CloseConfirm",
		Teml{
			"keyBindClose":   "esc",
			"keyBindConfirm": "enter",
		},
	)
	gui.renderString(g, "options", message)
	return nil
}

// HandleCredentialsPopup handles the views after executing a command that might ask for credentials
func (gui *Gui) HandleCredentialsPopup(g *gocui.Gui, popupOpened bool, cmdErr error) {
	if popupOpened {
		_, _ = gui.g.SetViewOnBottom("credentials")
	}
	if cmdErr != nil {
		errMessage := cmdErr.Error()
		if strings.Contains(errMessage, "Invalid username or password") {
			errMessage = gui.Tr.SLocalize("PassUnameWrong")
		}
		// we are not logging this error because it may contain a password
		_ = gui.createSpecificErrorPanel(errMessage, gui.getFilesView(), false)
	} else {
		_ = gui.closeConfirmationPrompt(g, true)
		_ = gui.refreshSidePanels(refreshOptions{mode: ASYNC})
	}
}
