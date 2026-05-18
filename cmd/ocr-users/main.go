// ocr-users is a small admin CLI for managing reviewer accounts in the
// validation web app's separate users SQLite DB.
//
// Subcommands: init, add, list, set-password, delete
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"

	"github.com/carlohamalainen/antarctic-database-go/users"
	"golang.org/x/term"
)

const defaultPath = "data/processed/ocr-users.sqlite3"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "init":
		err = runInit(args)
	case "add":
		err = runAdd(args)
	case "list":
		err = runList(args)
	case "set-password":
		err = runSetPassword(args)
	case "delete":
		err = runDelete(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: ocr-users <subcommand> [flags]

subcommands:
  init               create the users DB schema (idempotent)
  add <username>     create a new user (prompts for password)
  list               list all users
  set-password <u>   change a user's password
  delete <username>  delete a user and all their sessions

global flag:
  -db PATH    path to ocr-users.sqlite3 (default: `+defaultPath+`)
  -password   non-interactive password (insecure: visible in shell history)`)
}

func openDB(path string) (*sql.DB, error) {
	db, err := users.Open(path)
	if err != nil {
		return nil, err
	}
	if err := users.InitSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	path := fs.String("db", defaultPath, "users DB path")
	_ = fs.Parse(args)
	db, err := openDB(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	fmt.Printf("users DB ready at %s\n", *path)
	return nil
}

func runAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	path := fs.String("db", defaultPath, "users DB path")
	pwFlag := fs.String("password", "", "password (insecure; prompts if empty)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: ocr-users add <username>")
	}
	username := fs.Arg(0)

	pw, err := getPassword(*pwFlag, "Password for "+username+": ")
	if err != nil {
		return err
	}

	db, err := openDB(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := users.CreateUser(context.Background(), db, username, pw); err != nil {
		return err
	}
	fmt.Printf("created user %s\n", users.NormalizeUsername(username))
	return nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	path := fs.String("db", defaultPath, "users DB path")
	_ = fs.Parse(args)
	db, err := openDB(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := users.ListUsers(context.Background(), db)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("(no users)")
		return nil
	}
	for _, r := range rows {
		fmt.Printf("%-32s  created %s\n", r.Username, r.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	}
	return nil
}

func runSetPassword(args []string) error {
	fs := flag.NewFlagSet("set-password", flag.ExitOnError)
	path := fs.String("db", defaultPath, "users DB path")
	pwFlag := fs.String("password", "", "password (insecure; prompts if empty)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: ocr-users set-password <username>")
	}
	username := fs.Arg(0)
	pw, err := getPassword(*pwFlag, "New password for "+username+": ")
	if err != nil {
		return err
	}
	db, err := openDB(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := users.SetPassword(context.Background(), db, username, pw); err != nil {
		return err
	}
	fmt.Printf("password updated for %s\n", users.NormalizeUsername(username))
	return nil
}

func runDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	path := fs.String("db", defaultPath, "users DB path")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: ocr-users delete <username>")
	}
	username := users.NormalizeUsername(fs.Arg(0))
	if !*yes {
		fmt.Printf("Delete user %q and all their sessions? [y/N]: ", username)
		r := bufio.NewReader(os.Stdin)
		ans, _ := r.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("aborted")
			return nil
		}
	}
	db, err := openDB(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := users.DeleteUser(context.Background(), db, username); err != nil {
		return err
	}
	fmt.Printf("deleted user %s\n", username)
	return nil
}

// getPassword returns the explicit flag value if set; otherwise prompts twice
// (without echo) and verifies the two entries match.
func getPassword(flagValue, prompt string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", errors.New("stdin is not a terminal; use -password flag for non-interactive use")
	}
	fmt.Print(prompt)
	pw1, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Confirm: ")
	pw2, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(pw1) != string(pw2) {
		return "", errors.New("passwords do not match")
	}
	return string(pw1), nil
}
