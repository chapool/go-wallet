package probe

import (
	"github.com/spf13/cobra"
	"github/chapool/go-wallet/internal/util/command"
)

const (
	verboseFlag string = "verbose"
)

func New() *cobra.Command {
	return command.NewSubcommandGroup("probe",
		newLiveness(),
		newReadiness(),
	)
}
