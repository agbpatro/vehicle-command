package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teslamotors/vehicle-command/pkg/account"
	"github.com/teslamotors/vehicle-command/pkg/cli"
	"github.com/teslamotors/vehicle-command/pkg/protocol"
	"github.com/teslamotors/vehicle-command/pkg/protocol/protobuf/vcsec"
	"github.com/teslamotors/vehicle-command/pkg/vehicle"
)

var ErrCommandLineArgs = errors.New("invalid command line arguments")

type Argument struct {
	name string
	help string
}

type Handler func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error

type Command struct {
	help             string
	requiresAuth     bool // True if command requires client-to-vehicle authentication (private key)
	requiresFleetAPI bool // True if command requires client-to-server authentication (OAuth token)
	args             []Argument
	optional         []Argument
	handler          Handler
}

// configureAndVerifyFlags verifies that c contains all the information required to execute a command.
func configureFlags(c *cli.Config, commandName string, forceBLE bool) error {
	info, ok := commands[commandName]
	if !ok {
		return ErrUnknownCommand
	}
	c.Flags = cli.FlagBLE
	if info.requiresAuth {
		c.Flags |= cli.FlagPrivateKey | cli.FlagVIN
	}
	if !info.requiresFleetAPI {
		c.Flags |= cli.FlagVIN
	}
	if forceBLE {
		if info.requiresFleetAPI {
			return ErrRequiresOAuth
		}
	} else {
		c.Flags |= cli.FlagOAuth
	}

	// Verify all required parameters are present.
	havePrivateKey := !(c.KeyringKeyName == "" && c.KeyFilename == "")
	haveOAuth := !(c.KeyringTokenName == "" && c.TokenFilename == "")
	haveVIN := c.VIN != ""
	_, err := checkReadiness(commandName, havePrivateKey, haveOAuth, haveVIN)
	return err
}

var (
	ErrRequiresOAuth      = errors.New("command requires a FleetAPI OAuth token")
	ErrRequiresVIN        = errors.New("command requires a VIN")
	ErrRequiresPrivateKey = errors.New("command requires a private key")
	ErrUnknownCommand     = errors.New("unrecognized command")
)

func checkReadiness(commandName string, havePrivateKey, haveOAuth, haveVIN bool) (*Command, error) {
	info, ok := commands[commandName]
	if !ok {
		return nil, ErrUnknownCommand
	}
	if info.requiresFleetAPI {
		if !haveOAuth {
			return nil, ErrRequiresOAuth
		}
	} else {
		// Currently, commands supported by this application either target the account (and
		// therefore require FleetAPI credentials but not a VIN) or target a vehicle (and therefore
		// require a VIN but not FleetAPI credentials).
		if !haveVIN {
			return nil, ErrRequiresVIN
		}
	}
	if info.requiresAuth && !havePrivateKey {
		return nil, ErrRequiresPrivateKey
	}
	return info, nil
}

func execute(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args []string) error {
	if len(args) == 0 {
		return errors.New("missing COMMAND")
	}

	info, err := checkReadiness(args[0], car != nil && car.PrivateKeyAvailable(), acct != nil, car != nil)
	if err != nil {
		return err
	}

	if len(args)-1 < len(info.args) || len(args)-1 > len(info.args)+len(info.optional) {
		writeErr("Invalid number of command line arguments: %d (%d required, %d optional).", len(args), len(info.args), len(info.optional))
		err = ErrCommandLineArgs
	} else {
		keywords := make(map[string]string)
		for i, argInfo := range info.args {
			keywords[argInfo.name] = args[i+1]
		}
		index := len(info.args) + 1
		for _, argInfo := range info.optional {
			if index >= len(args) {
				break
			}
			keywords[argInfo.name] = args[index]
			index++
		}
		err = info.handler(ctx, acct, car, keywords)
	}

	// Print command-specific help
	if errors.Is(err, ErrCommandLineArgs) {
		info.Usage(args[0])
	}
	return err
}

func (c *Command) Usage(name string) {
	fmt.Printf("Usage: %s", name)
	maxLength := 0
	for _, arg := range c.args {
		fmt.Printf(" %s", arg.name)
		if len(arg.name) > maxLength {
			maxLength = len(arg.name)
		}
	}
	if len(c.optional) > 0 {
		fmt.Printf(" [")
	}
	for _, arg := range c.optional {
		fmt.Printf(" %s", arg.name)
		if len(arg.name) > maxLength {
			maxLength = len(arg.name)
		}
	}
	if len(c.optional) > 0 {
		fmt.Printf(" ]")
	}
	fmt.Printf("\n%s\n", c.help)
	maxLength++
	for _, arg := range c.args {
		fmt.Printf("    %s:%s%s\n", arg.name, strings.Repeat(" ", maxLength-len(arg.name)), arg.help)
	}
	for _, arg := range c.optional {
		fmt.Printf("    %s:%s%s\n", arg.name, strings.Repeat(" ", maxLength-len(arg.name)), arg.help)
	}
}

var commands = map[string]*Command{
	"unlock": {
		help:             "Unlock vehicle",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.Unlock(ctx)
		},
	},
	"lock": {
		help:             "Lock vehicle",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.Lock(ctx)
		},
	},
	"drive": {
		help:             "Remote start vehicle",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.RemoteDrive(ctx)
		},
	},
	"climate-on": {
		help:             "Turn on climate control",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.ClimateOn(ctx)
		},
	},
	"climate-off": {
		help:             "Turn off climate control",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.ClimateOff(ctx)
		},
	},
	"climate-set-temp": {
		help:             "Set temperature (Celsius)",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "TEMP", help: "Desired temperature (e.g., 70f or 21c; defaults to Celsius)"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			var degrees float32
			var unit string
			if _, err := fmt.Sscanf(args["TEMP"], "%f%s", &degrees, &unit); err != nil {
				return fmt.Errorf("failed to parse temperature: format as 22C or 72F")
			}
			if unit == "F" || unit == "f" {
				degrees = (5.0 * degrees / 9.0) + 32.0
			} else if unit != "C" && unit != "c" {
				return fmt.Errorf("temperature units must be C or F")
			}
			return car.ChangeClimateTemp(ctx, degrees, degrees)
		},
	},
	"add-key": {
		help:             "Add PUBLIC_KEY to vehicle whitelist with ROLE and FORM_FACTOR",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "PUBLIC_KEY", help: "file containing public key (or corresponding private key)"},
			{name: "ROLE", help: "One of: owner, driver"},
			{name: "FORM_FACTOR", help: "One of: nfc_card, ios_device, android_device, cloud_key"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			role := strings.ToUpper(args["ROLE"])
			if role != "OWNER" && role != "DRIVER" {
				return fmt.Errorf("%w: invalid ROLE", ErrCommandLineArgs)
			}
			formFactor, ok := vcsec.KeyFormFactor_value["KEY_FORM_FACTOR_"+strings.ToUpper(args["FORM_FACTOR"])]
			if !ok {
				return fmt.Errorf("%w: unrecognized FORM_FACTOR", ErrCommandLineArgs)
			}
			publicKey, err := protocol.LoadPublicKey(args["PUBLIC_KEY"])
			if err != nil {
				return fmt.Errorf("invalid public key: %s", err)
			}
			return car.AddKey(ctx, publicKey, role == "OWNER", vcsec.KeyFormFactor(formFactor))
		},
	},
	"add-key-request": {
		help:             "Requset NFC-card approval for a enrolling PUBLIC_KEY with ROLE and FORM_FACTOR",
		requiresAuth:     false,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "PUBLIC_KEY", help: "file containing public key (or corresponding private key)"},
			{name: "ROLE", help: "One of: owner, driver"},
			{name: "FORM_FACTOR", help: "One of: nfc_card, ios_device, android_device, cloud_key"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			role := strings.ToUpper(args["ROLE"])
			if role != "OWNER" && role != "DRIVER" {
				return fmt.Errorf("%w: invalid ROLE", ErrCommandLineArgs)
			}
			formFactor, ok := vcsec.KeyFormFactor_value["KEY_FORM_FACTOR_"+strings.ToUpper(args["FORM_FACTOR"])]
			if !ok {
				return fmt.Errorf("%w: unrecognized FORM_FACTOR", ErrCommandLineArgs)
			}
			publicKey, err := protocol.LoadPublicKey(args["PUBLIC_KEY"])
			if err != nil {
				return fmt.Errorf("invalid public key: %s", err)
			}
			if err := car.SendAddKeyRequest(ctx, publicKey, role == "OWNER", vcsec.KeyFormFactor(formFactor)); err != nil {
				return err
			}
			fmt.Printf("Sent add-key request to %s. Confirm by tapping NFC card on center console.\n", car.VIN())
			return nil
		},
	},
	"remove-key": {
		help:             "Remove PUBLIC_KEY from vehicle whitelist",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "PUBLIC_KEY", help: "file containing public key (or corresponding private key)"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			publicKey, err := protocol.LoadPublicKey(args["PUBLIC_KEY"])
			if err != nil {
				return fmt.Errorf("invalid public key: %s", err)
			}
			return car.RemoveKey(ctx, publicKey)
		},
	},
	"rename-key": {
		help:             "Change the human-readable metadata of PUBLIC_KEY to NAME, MODEL, KIND",
		requiresAuth:     false,
		requiresFleetAPI: true,
		args: []Argument{
			{name: "PUBLIC_KEY", help: "file containing public key (or corresponding private key)"},
			{name: "NAME", help: "New human-readable name for the public key (e.g., Dave's Phone)"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			publicKey, err := protocol.LoadPublicKey(args["PUBLIC_KEY"])
			if err != nil {
				return fmt.Errorf("invalid public key: %s", err)
			}
			return acct.UpdateKey(ctx, publicKey, args["NAME"])
		},
	},
	"get": {
		help:             "GET an owner API http ENDPOINT. Hostname will be taken from -config.",
		requiresAuth:     false,
		requiresFleetAPI: true,
		args: []Argument{
			{name: "ENDPOINT", help: "Fleet API endpoint"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			reply, err := acct.Get(ctx, args["ENDPOINT"])
			if err != nil {
				return err
			}
			fmt.Println(string(reply))
			return nil
		},
	},
	"post": {
		help:             "POST to ENDPOINT the contents of FILE. Hostname will be taken from -config.",
		requiresAuth:     false,
		requiresFleetAPI: true,
		args: []Argument{
			{name: "ENDPOINT", help: "Fleet API endpoint"},
		},
		optional: []Argument{
			{name: "FILE", help: "JSON file to POST"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			var jsonBytes []byte
			var err error
			if filename, ok := args["FILE"]; ok {
				jsonBytes, err = os.ReadFile(filename)
			} else {
				jsonBytes, err = io.ReadAll(os.Stdin)
			}
			if err != nil {
				return err
			}
			reply, err := acct.Post(ctx, args["ENDPOINT"], jsonBytes)
			// reply can be set where there's an error; typically a JSON blob providing details
			if reply != nil {
				fmt.Println(string(reply))
			}
			if err != nil {
				return err
			}
			return nil
		},
	},
	"list-keys": {
		help:             "List public keys enrolled on vehicle",
		requiresAuth:     false,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			summary, err := car.KeySummary(ctx)
			if err != nil {
				return err
			}
			slot := uint32(0)
			var details *vcsec.WhitelistEntryInfo
			for mask := summary.GetSlotMask(); mask > 0; mask >>= 1 {
				if mask&1 == 1 {
					details, err = car.KeyInfoBySlot(ctx, slot)
					if err != nil {
						writeErr("Error fetching slot %d: %s", slot, err)
						if errors.Is(err, context.DeadlineExceeded) {
							return err
						}
					}
					if details != nil {
						fmt.Printf("%02x\t%s\t%s\n", details.GetPublicKey().GetPublicKeyRaw(), details.GetKeyRole(), details.GetMetadataForKey().GetKeyFormFactor())
					}
				}
				slot++
			}
			return nil
		},
	},
	"honk": {
		help:             "Honk horn",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.HonkHorn(ctx)
		},
	},
	"ping": {
		help:             "Ping vehicle",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.Ping(ctx)
		},
	},
	"flash-lights": {
		help:         "Flash lights",
		requiresAuth: true,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.FlashLights(ctx)
		},
	},
	"charging-set-limit": {
		help:             "Set charge limit to PERCENT",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "PERCENT", help: "Charging limit"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			limit, err := strconv.Atoi(args["PERCENT"])
			if err != nil {
				return fmt.Errorf("error parsing PERCENT")
			}
			return car.ChangeChargeLimit(ctx, int32(limit))
		},
	},
	"charging-set-amps": {
		help:             "Set charge current to AMPS",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "AMPS", help: "Charging current"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			limit, err := strconv.Atoi(args["AMPS"])
			if err != nil {
				return fmt.Errorf("error parsing AMPS")
			}
			return car.SetChargingAmps(ctx, int32(limit))
		},
	},
	"charging-start": {
		help:             "Start charging",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.ChargeStart(ctx)
		},
	},
	"charging-stop": {
		help:             "Stop charging",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.ChargeStop(ctx)
		},
	},
	"charging-schedule": {
		help:             "Schedule charging to MINS minutes after midnight and enable daily scheduling",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "MINS", help: "Time after midnight in minutes"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			minutesAfterMidnight, err := strconv.Atoi(args["MINS"])
			if err != nil {
				return fmt.Errorf("error parsing minutes")
			}
			// Convert minutes to a time.Duration
			chargingTime := time.Duration(minutesAfterMidnight) * time.Minute
			return car.ScheduleCharging(ctx, true, chargingTime)
		},
	},
	"charging-schedule-cancel": {
		help:             "Cancel scheduled charge start",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.ScheduleCharging(ctx, false, 0*time.Hour)
		},
	},
	"media-set-volume": {
		help:             "Set volume",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "VOLUME", help: "Set volume (0.0-10.0"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			volume, err := strconv.ParseFloat(args["VOLUME"], 32)
			if err != nil {
				return fmt.Errorf("failed to parse volume")
			}
			return car.SetVolume(ctx, float32(volume))
		},
	},
	"software-update-start": {
		help:             "Start software update after DELAY",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{
				name: "DELAY",
				help: "Time to wait before starting update. Examples: 2h, 10m.",
			},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			delay, err := time.ParseDuration(args["DELAY"])
			if err != nil {
				return fmt.Errorf("error parsing DELAY. Valid times are <n><unit>, where <n> is a number (decimals are allowed) and <unit> is 's, 'm', or 'h'")
				// ...or 'ns'/'µs' if that's your cup of tea.
			}
			return car.ScheduleSoftwareUpdate(ctx, delay)
		},
	},
	"software-update-cancel": {
		help:             "Cancel a pending software update",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.CancelSoftwareUpdate(ctx)
		},
	},
	"sentry-mode": {
		help:             "Set sentry mode to STATE ('on' or 'off')",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "STATE", help: "'on' or 'off'"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			var state bool
			switch args["STATE"] {
			case "on":
				state = true
			case "off":
				state = false
			default:
				return fmt.Errorf("sentry mode state must be 'on' or 'off'")
			}
			return car.SetSentryMode(ctx, state)
		},
	},
	"wake": {
		help:             "Wake up vehicle",
		requiresAuth:     false,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.Wakeup(ctx)
		},
	},
	"trunk-open": {
		help:             "Open vehicle trunk. Note that trunk-close only works on certain vehicle types.",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.OpenTrunk(ctx)
		},
	},
	"trunk-move": {
		help:             "Toggle trunk open/closed. Closing is only available on certain vehicle types.",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.ActuateTrunk(ctx)
		},
	},
	"trunk-close": {
		help:             "Closes vehicle trunk. Only available on certain vehicle types.",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.CloseTrunk(ctx)
		},
	},
	"frunk-open": {
		help:             "Open vehicle frunk. Note that there's no frunk-close command!",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.OpenFrunk(ctx)
		},
	},
	"charge-port-open": {
		help:             "Open charge port",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.OpenChargePort(ctx)
		},
	},
	"charge-port-close": {
		help:             "Close charge port",
		requiresAuth:     true,
		requiresFleetAPI: false,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.CloseChargePort(ctx)
		},
	},
	"autosecure-modelx": {
		help: "Close falcon-wing doors and lock vehicle. Model X only.",
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			return car.AutoSecureVehicle(ctx)
		},
	},
	"session-info": {
		help:             "Retrieve session info for PUBLIC_KEY from DOMAIN",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "PUBLIC_KEY", help: "file containing public key (or corresponding private key)"},
			{name: "DOMAIN", help: "'vcsec' or 'infotainment'"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			// See SeatPosition definition for controlling backrest heaters (limited models).
			domains := map[string]protocol.Domain{
				"vcsec":        protocol.DomainVCSEC,
				"infotainment": protocol.DomainInfotainment,
			}
			domain, ok := domains[args["DOMAIN"]]
			if !ok {
				return fmt.Errorf("invalid domain %s", args["DOMAIN"])
			}
			publicKey, err := protocol.LoadPublicKey(args["PUBLIC_KEY"])
			if err != nil {
				return fmt.Errorf("invalid public key: %s", err)
			}
			info, err := car.SessionInfo(ctx, publicKey, domain)
			if err != nil {
				return err
			}
			fmt.Printf("%s\n", info)
			return nil
		},
	},
	"seat-heater": {
		help:             "Set seat heater at POSITION to LEVEL",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "SEAT", help: "<front|2nd-row|3rd-row>-<left|center|right> (e.g., 2nd-row-left)"},
			{name: "LEVEL", help: "off, low, medium, or high"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			// See SeatPosition definition for controlling backrest heaters (limited models).
			seats := map[string]vehicle.SeatPosition{
				"front-left":     vehicle.SeatFrontLeft,
				"front-right":    vehicle.SeatFrontRight,
				"2nd-row-left":   vehicle.SeatSecondRowLeft,
				"2nd-row-center": vehicle.SeatSecondRowCenter,
				"2nd-row-right":  vehicle.SeatSecondRowRight,
				"3rd-row-left":   vehicle.SeatThirdRowLeft,
				"3rd-row-right":  vehicle.SeatThirdRowRight,
			}
			position, ok := seats[args["SEAT"]]
			if !ok {
				return fmt.Errorf("invalid seat position")
			}
			levels := map[string]vehicle.Level{
				"off":    vehicle.LevelOff,
				"low":    vehicle.LevelLow,
				"medium": vehicle.LevelMed,
				"high":   vehicle.LevelHigh,
			}
			level, ok := levels[args["LEVEL"]]
			if !ok {
				return fmt.Errorf("invalid seat heater level")
			}
			spec := map[vehicle.SeatPosition]vehicle.Level{
				position: level,
			}
			return car.SetSeatHeater(ctx, spec)
		},
	},
	"steering-wheel-heater": {
		help:             "Set steering wheel mode to STATE ('on' or 'off')",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "STATE", help: "'on' or 'off'"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			var state bool
			switch args["STATE"] {
			case "on":
				state = true
			case "off":
				state = false
			default:
				return fmt.Errorf("steering wheel state must be 'on' or 'off'")
			}
			return car.SetSteeringWheelHeater(ctx, state)
		},
	},
	"product-info": {
		help:             "Print JSON product info",
		requiresAuth:     false,
		requiresFleetAPI: true,
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			productsJSON, err := acct.Get(ctx, "api/1/products")
			if err != nil {
				return err
			}
			fmt.Println(string(productsJSON))
			return nil
		},
	},
	"auto-seat-and-climate": {
		help:             "Turn on automatic seat heating and HVAC",
		requiresAuth:     true,
		requiresFleetAPI: false,
		args: []Argument{
			{name: "POSITIONS", help: "'L' (left), 'R' (right), or 'LR'"},
		},
		optional: []Argument{
			{name: "STATE", help: "'on' (default) or 'off'"},
		},
		handler: func(ctx context.Context, acct *account.Account, car *vehicle.Vehicle, args map[string]string) error {
			var positions []vehicle.SeatPosition
			if strings.Contains(args["POSITIONS"], "L") {
				positions = append(positions, vehicle.SeatFrontLeft)
			}
			if strings.Contains(args["POSITIONS"], "R") {
				positions = append(positions, vehicle.SeatFrontRight)
			}
			if len(positions) != len(args["POSITIONS"]) {
				return fmt.Errorf("invalid seat position")
			}
			enabled := true
			if state, ok := args["STATE"]; ok && strings.ToUpper(state) == "OFF" {
				enabled = false
			}
			return car.AutoSeatAndClimate(ctx, positions, enabled)
		},
	},
}
