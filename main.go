package main

import (
	"fmt"
	"os"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run wires global flags, loads the app context and dispatches one command.
func run(argv []string) int {
	glob, rest, err := splitGlobal(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("cli.fatal", err))
		return 2
	}
	// The language flag has to be honoured before anything else prints.
	if v, ok := glob.values["lang"]; ok {
		if err := i18n.Set(v); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", i18n.T("cli.fatal", err))
			return 2
		}
	}

	app, err := newApp(glob)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T("cli.fatal", err))
		return 1
	}
	defer app.Close()

	if len(rest) == 0 {
		if glob.boolean("help") {
			printHelp(app, "")
			return 0
		}
		app.Log.Printf("%s", i18n.T("cli.no_command"))
		app.Log.Printf("%s", i18n.T("cli.usage"))
		return 2
	}

	command, args := rest[0], rest[1:]
	switch command {
	case "help":
		topic := ""
		if len(args) > 0 {
			topic = args[0]
		}
		printHelp(app, topic)
		return 0
	case "version", "--version":
		return app.cmdVersion()
	case "login":
		return app.cmdLogin(args)
	case "logout":
		return app.cmdLogout(args)
	case "list", "ls":
		return app.cmdList(args)
	case "account", "accounts", "acc":
		return app.cmdAccount(args)
	case "create":
		return app.cmdCreate(args)
	case "delete", "remove", "rm":
		return app.cmdDelete(args)
	case "launch", "run", "start":
		return app.cmdLaunch(args)
	case "refresh":
		return app.cmdRefresh(args)
	case "config", "cfg":
		return app.cmdConfig(args)
	case "java", "jdk":
		return app.cmdJava(args)
	case "instance", "inst":
		return app.cmdInstance(args)
	case "doctor":
		return app.cmdDoctor(args)
	case "repair", "fix":
		return app.cmdRepair(args)
	default:
		app.Log.Printf("%s", i18n.T("cli.unknown_command", command))
		app.Log.Printf("%s", i18n.T("cli.usage"))
		app.Log.Printf("%s", i18n.T("cli.help_hint"))
		return 2
	}
}

// printHelp renders the command list or one command's help.
func printHelp(app *App, topic string) {
	type cmd struct {
		name  string
		usage string
		desc  string
	}
	cmds := []cmd{
		{"login", "cli.cmd.login.usage", "cli.cmd.login.desc"},
		{"logout", "cli.cmd.logout.usage", "cli.cmd.logout.desc"},
		{"account", "cli.cmd.account.usage", "cli.cmd.account.desc"},
		{"list", "cli.cmd.list.usage", "cli.cmd.list.desc"},
		{"create", "cli.cmd.create.usage", "cli.cmd.create.desc"},
		{"instance", "cli.cmd.instance.usage", "cli.cmd.instance.desc"},
		{"doctor", "cli.cmd.doctor.usage", "cli.cmd.doctor.desc"},
		{"repair", "cli.cmd.repair.usage", "cli.cmd.repair.desc"},
		{"delete", "cli.cmd.delete.usage", "cli.cmd.delete.desc"},
		{"launch", "cli.cmd.launch.usage", "cli.cmd.launch.desc"},
		{"refresh", "cli.cmd.refresh.usage", "cli.cmd.refresh.desc"},
		{"config", "cli.cmd.config.usage", "cli.cmd.config.desc"},
		{"java", "cli.cmd.java.usage", "cli.cmd.java.desc"},
		{"version", "cli.cmd.version.usage", "cli.cmd.version.desc"},
		{"help", "cli.help_usage", "cli.help_desc"},
	}
	if topic == "" {
		app.Log.Printf("%s", i18n.T("cli.about"))
		app.Log.Printf("%s", i18n.T("cli.usage"))
		app.Log.Printf("")
		app.Log.Printf("%s", i18n.T("cli.commands_title"))
		for _, c := range cmds {
			app.Log.Printf("  %-8s %s", c.name, i18n.T(c.desc))
		}
		app.Log.Printf("")
		app.Log.Printf("%s", i18n.T("cli.global_flags"))
		app.Log.Printf("%s", i18n.T("cli.help_hint"))
		return
	}
	for _, c := range cmds {
		if c.name == topic {
			app.Log.Printf("%s", i18n.T(c.usage))
			app.Log.Printf("  %s", i18n.T(c.desc))
			return
		}
	}
	app.Log.Printf("%s", i18n.T("cli.unknown_command", topic))
}

// fatal prints an error and returns exit code 1.
func (a *App) fatal(err error) int {
	a.Log.Errorf("%s", err)
	a.Log.Printf("%s", i18n.T("cli.fatal", err))
	return 1
}

// usageError reports a bad invocation.
func (a *App) usageError(format string, args ...interface{}) int {
	a.Log.Printf("%s", i18n.T(format, args...))
	a.Log.Printf("%s", i18n.T("cli.help_hint"))
	return 2
}

func (a *App) cmdVersion() int {
	a.Log.Printf("mcli %s", Version)
	a.Log.Printf("%s", i18n.T("cli.mcl_home", a.Paths.Home))
	a.Log.Printf("%s", i18n.T("cli.config_path", a.Store.Path))
	a.Log.Printf("%s", i18n.T("cli.state_path", a.Store.StatePath))
	a.Log.Printf("%s", i18n.T("cli.minecraft_dir", a.MinecraftDir()))
	a.Log.Printf("%s", i18n.T("auth.summary", a.accountSummary()))
	return 0
}
