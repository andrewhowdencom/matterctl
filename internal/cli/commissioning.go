package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func discoverCommand(config *viper.Viper) *cobra.Command {
	return &cobra.Command{
		Use: "discover", Short: "Discover network devices advertising an open Matter commissioning window", Args: cobra.NoArgs,
		Example: "  matterctl device discover\n  matterctl device discover --output json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			result, err := service.Discover(cmd.Context())
			if err != nil {
				return err
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), result)
			}
			if len(result.Devices) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "No commissionable network devices found. Open a sharing window in the existing controller, then try again.")
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "INSTANCE\tNAME\tVENDOR\tPRODUCT\tDISCRIMINATOR"); err != nil {
				return err
			}
			for _, device := range result.Devices {
				if _, err := fmt.Fprintf(w, "%q\t%q\t%s\t%s\t%s\n", device.InstanceName, device.DeviceName, optionalNumber(device.VendorID), optionalNumber(device.ProductID), optionalNumber(device.LongDiscriminator)); err != nil {
					return err
				}
			}
			return w.Flush()
		},
	}
}

func optionalNumber[T int | uint16](value *T) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprint(*value)
}

func commissionCommand(config *viper.Viper) *cobra.Command {
	var code, name string
	var stdin, networkOnly bool
	var timeout time.Duration
	command := &cobra.Command{
		Use: "commission", Short: "Add a shared Matter device already on the network", Args: cobra.NoArgs,
		Long:    "Add a device using a fresh sharing code from its existing controller. Only network-only commissioning is supported. Read a code with --code-stdin to keep it out of command arguments and shell history. Cancellation stops waiting; Matter Server may continue commissioning. Check device list before retrying an uncertain result.",
		Example: "  matterctl device commission --code-stdin --network-only\n  matterctl device commission --code '<fresh-sharing-code>' --timeout 5m",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, format, err := configuredClient(config)
			if err != nil {
				return err
			}
			if stdin == cmd.Flags().Changed("code") {
				return errors.New("provide exactly one of --code or --code-stdin")
			}
			if timeout < time.Second || timeout > api.MaxCommissionTimeout {
				return errors.New("commissioning timeout must be between 1s and 10m")
			}
			if !networkOnly {
				return errors.New("only network-only commissioning is supported")
			}
			pairingCode := code
			if stdin {
				input, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1025))
				if err != nil {
					return errors.New("read sharing code from stdin")
				}
				pairingCode = string(input)
			}
			pairingCode, err = api.NormalizePairingCode(pairingCode)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("name") && format != "json" {
				name, err = promptDeviceName()
				if err != nil {
					return err
				}
			}
			if name != "" {
				if err := (api.LabelRequest{Target: "pending", Label: name}).Validate(); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Waiting for network-only commissioning (up to %s)...\n", timeout); err != nil {
				return err
			}
			device, err := service.Commission(cmd.Context(), api.CommissionRequest{Code: pairingCode, NetworkOnly: true, TimeoutMS: timeout.Milliseconds()})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "Commissioning completed."); err != nil {
				return err
			}
			if name != "" {
				label, labelErr := service.SetLabelAfterCommission(cmd.Context(), api.LabelRequest{Target: string(device.NodeID), Label: name})
				if labelErr != nil {
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Device added as node %s, but its name was not confirmed: %v. Set it later with device label set %s %q.\n", device.NodeID, labelErr, device.NodeID, name); err != nil {
						return err
					}
				} else {
					device.Label = label.Label
				}
			}
			if format == "json" {
				return printJSON(cmd.OutOrStdout(), device)
			}
			return printDevice(cmd.OutOrStdout(), device)
		},
	}
	// Codes deliberately bypass Viper: no configuration or environment storage.
	command.Flags().StringVar(&code, "code", "", "Fresh manual sharing code or MT: QR payload (prefer --code-stdin)")
	command.Flags().StringVar(&name, "name", "", "Matter device name (prompt on a terminal if omitted; empty skips naming)")
	command.Flags().BoolVar(&stdin, "code-stdin", false, "Read the sharing code from standard input, ending at EOF")
	command.Flags().BoolVar(&networkOnly, "network-only", true, "Commission over the existing IP network; must remain true")
	command.Flags().DurationVar(&timeout, "timeout", api.CommissionTimeout, "Overall wait for commissioning (1s to 10m)")
	command.MarkFlagsMutuallyExclusive("code", "code-stdin")
	return command
}

func promptDeviceName() (string, error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", nil // Noninteractive callers can use --name.
	}
	defer func() { _ = terminal.Close() }()
	if _, err := fmt.Fprint(terminal, "Device name (optional, Enter to skip): "); err != nil {
		return "", err
	}
	name, err := bufio.NewReader(terminal).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(name, "\n"), "\r"), nil
}
