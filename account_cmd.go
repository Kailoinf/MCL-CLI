package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// ---------------------------------------------------------------------------
// mcli account [list | use <name> | remove <name>]
//
// `mcli login` stores an account, but a launcher also has to answer "who am I
// logged in as, and which one will be used?" without grepping state.toml.

func (a *App) cmdAccount(args []string) int {
	if len(args) == 0 {
		return a.accountList()
	}
	action, rest := args[0], args[1:]
	switch action {
	case "list", "ls", "--list":
		return a.accountList()
	case "use", "switch", "set", "active":
		return a.accountUse(rest)
	case "remove", "rm", "delete", "logout":
		return a.cmdLogout(rest)
	case "help", "--help":
		a.Log.Printf("%s", i18n.T("cli.cmd.account.usage"))
		return 0
	default:
		return a.fatal(fmt.Errorf("%s", i18n.T("cli.unknown_command", action)))
	}
}

func (a *App) accountList() int {
	names := a.DB.AccountNames()
	if len(names) == 0 {
		a.Log.Printf("%s", i18n.T("auth.no_account"))
		a.Log.Printf("%s", i18n.T("auth.no_account_hint"))
		return 0
	}
	a.Log.Printf("%s", i18n.T("auth.list_header",
		padRight("", 1), padRight("NAME", 18), padRight("UUID", 36),
		padRight("LABEL", 18), "TOKEN EXPIRES"))
	for _, uuid := range names {
		acc := a.DB.Data.Accounts[uuid]
		marker := " "
		if acc.Active {
			marker = "*"
		}
		a.Log.Printf("%s", i18n.T("auth.list_row",
			marker, padRight(acc.Name, 18), padRight(acc.UUID, 36),
			padRight(acc.Label, 18), a.tokenState(acc)))
	}
	a.Log.Printf("%s", i18n.T("auth.list_hint"))
	return 0
}

// tokenState describes one account's stored credentials without ever printing
// one: when the Minecraft token expires, and whether a refresh token is there.
func (a *App) tokenState(acc *store.Account) string {
	tok, session := a.DB.Tokens(acc.UUID)
	if tok.RefreshEnc == "" {
		return i18n.T("auth.state_no_token")
	}
	if session.MCTokenExp.IsZero() || session.MCTokenEnc == "" {
		return i18n.T("auth.state_session_missing")
	}
	left := time.Until(session.MCTokenExp)
	if left <= 0 {
		return i18n.T("auth.state_expired", session.MCTokenExp.Format("2006-01-02 15:04"))
	}
	return i18n.T("auth.state_valid", session.MCTokenExp.Format("2006-01-02 15:04"),
		humanDuration(left))
}

// humanDuration renders a coarse "in 3h 20m" style remainder.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return i18n.T("common.seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return i18n.T("common.minutes", int(d.Minutes()))
	}
	h := int(d.Hours())
	if h < 48 {
		return i18n.T("common.hours", h)
	}
	return i18n.T("common.days", h/24)
}

func (a *App) accountUse(args []string) int {
	f, err := parseFlags("account use", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.account.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "account use", "<name|uuid|label>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "account use")
	}
	acc := a.findAccount(f.args[0])
	if acc == nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("auth.account_not_found", f.args[0])))
	}
	a.DB.SetActive(acc.UUID)
	if err := a.DB.Save(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("auth.active_set", acc.Name, acc.UUID))
	tok, _ := a.DB.Tokens(acc.UUID)
	if tok.RefreshEnc == "" {
		a.Log.Warnf("%s", i18n.T("auth.state_no_token"))
	}
	return 0
}

// accountSummary is a one-line description used by `mcli version` and the
// doctor command: which account will be used, or that there is none.
func (a *App) accountSummary() string {
	acc := a.DB.ActiveAccount()
	if acc == nil {
		if n := len(a.DB.Data.Accounts); n > 0 {
			return i18n.T("auth.summary_none_active", n)
		}
		return i18n.T("auth.no_account")
	}
	return strings.TrimSpace(fmt.Sprintf("%s (%s)", acc.Name, acc.UUID))
}
