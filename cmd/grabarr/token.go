package main

import (
	"errors"
	"fmt"
	"io"

	"grabarr/internal/auth"
	"grabarr/internal/config"
	"grabarr/internal/repository"
)

const tokenUsage = `usage: grabarr token create NAME | list | revoke NAME

API tokens let scripts call the API with "Authorization: Bearer <token>".
In production run it inside the container:
  docker exec grabarr /app/grabarr token create scripts`

// tokenStore is what the token command needs from the repository.
type tokenStore interface {
	CreateAPIToken(name, tokenHash string) error
	ListAPITokens() ([]repository.APIToken, error)
	DeleteAPIToken(name string) error
}

// tokenCommand manages API tokens against the configured database.
func tokenCommand(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(getConfigPath())
	if err != nil {
		fmt.Fprintln(stderr, "load config:", err)
		return 1
	}
	repo, err := repository.New(cfg.GetDatabase().Path)
	if err != nil {
		fmt.Fprintln(stderr, "open database:", err)
		return 1
	}
	defer repo.Close()
	return runTokenCommand(repo, args, stdout, stderr)
}

func runTokenCommand(store tokenStore, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, tokenUsage)
		return 2
	}
	switch {
	case args[0] == "create" && len(args) == 2:
		tok := auth.NewToken()
		if err := store.CreateAPIToken(args[1], auth.HashToken(tok)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		// Shown once; only its hash is stored.
		fmt.Fprintln(stdout, tok)
		return 0
	case args[0] == "list" && len(args) == 1:
		tokens, err := store.ListAPITokens()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, t := range tokens {
			used := "never used"
			if t.LastUsedAt != nil {
				used = "last used " + t.LastUsedAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(stdout, "%s\tcreated %s\t%s\n", t.Name, t.CreatedAt.Format("2006-01-02"), used)
		}
		return 0
	case args[0] == "revoke" && len(args) == 2:
		if err := store.DeleteAPIToken(args[1]); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				fmt.Fprintf(stderr, "no token named %q\n", args[1])
			} else {
				fmt.Fprintln(stderr, err)
			}
			return 1
		}
		fmt.Fprintf(stdout, "revoked %s\n", args[1])
		return 0
	}
	fmt.Fprintln(stderr, tokenUsage)
	return 2
}
