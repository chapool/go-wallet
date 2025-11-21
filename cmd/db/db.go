package db

import (
	"github.com/spf13/cobra"
	"github/chapool/go-wallet/internal/util/command"
)

func New() *cobra.Command {
	return command.NewSubcommandGroup("db",
		newMigrate(),
		newSeed(),
	)
}
