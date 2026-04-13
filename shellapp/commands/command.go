// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package commands

import (
	"errors"
	"strings"
)

type Signal int

const (
	SignalNone                 Signal = 0
	SignalExit                 Signal = 1
	SignalClear                Signal = 2
	SignalRefreshRooms         Signal = 3
	SignalRoomSelected         Signal = 4
	SignalConnected            Signal = 5
	SignalJump                 Signal = 6
	SignalNeedPassword         Signal = 7
	SignalNeedRegisterPassword Signal = 8
	SignalStartDM              Signal = 9
	SignalVoiceLeave           Signal = 10
)

// DMUser holds the target username for the most recent StartDM signal.
var DMUser string

type ArgDef struct {
	Short string // e.g. "f"
	Long  string // e.g. "force"
	Desc  string
}

type CommandDef struct {
	Name string
	Desc string
	Args []ArgDef
}

var Registry = []CommandDef{
	{Name: CmdClear, Desc: "clear the screen"},
	{Name: CmdExit, Desc: "exit the shell"},
	{Name: CmdRegister, Desc: "register a user ID with a broker (must be done before connect)", Args: []ArgDef{
		{Short: "b", Long: "broker", Desc: "broker base URL (e.g. localhost:7070)"},
		{Short: "u", Long: "user", Desc: "user ID to register"},
	}},
	{Name: CmdLogin, Desc: "authenticate with a broker and establish a WebSocket connection (prompts for password)", Args: []ArgDef{
		{Short: "b", Long: "broker", Desc: "broker base URL (e.g. localhost:7070)"},
		{Short: "u", Long: "user", Desc: "username to log in as (must be registered first)"},
	}},
	{Name: CmdEcho, Desc: "send a message to the broker and receive it back (max 1024 chars)", Args: []ArgDef{
		{Short: "m", Long: "message", Desc: "message text to echo"},
	}},
	{Name: CmdPing, Desc: "test connectivity to a broker via WebSocket", Args: []ArgDef{
		{Short: "b", Long: "broker", Desc: "broker base URL (e.g. localhost:7070)"},
	}},
	{Name: CmdWcid, Desc: "describe all commands and their arguments", Args: []ArgDef{
		{Short: "c", Long: "command", Desc: "show details for a specific command"},
	}},
	{Name: CmdRoomCreate, Desc: "create a new room; generates an AES-256 room key and encrypts it with your public key", Args: []ArgDef{
		{Short: "n", Long: "name", Desc: "room name"},
	}},
	{Name: CmdRoomList, Desc: "list all rooms you have joined"},
	{Name: CmdRoomSelect, Desc: "select a room by name or ID (sets the active room for invite and members)", Args: []ArgDef{
		{Short: "r", Long: "room", Desc: "room name or ID prefix"},
	}},
	{Name: CmdRoomInvite, Desc: "invite a user to the currently selected room", Args: []ArgDef{
		{Short: "u", Long: "user", Desc: "username to invite"},
	}},
	{Name: CmdRoomInvites, Desc: "list pending room invitations sent to you"},
	{Name: CmdRoomAccept, Desc: "accept a pending room invitation", Args: []ArgDef{
		{Short: "r", Long: "room", Desc: "room ID to accept"},
	}},
	{Name: CmdSay, Desc: "send a message to the currently selected room", Args: []ArgDef{
		{Short: "m", Long: "message", Desc: "message text to send"},
	}},
	{Name: CmdJump, Desc: "jump to a specific line in the conversation history", Args: []ArgDef{
		{Short: "L", Long: "line", Desc: "target line number (1-indexed from top)"},
	}},
	{Name: CmdVoiceJoin, Desc: "join a voice call in the currently selected room"},
	{Name: CmdVoiceLeave, Desc: "leave the active voice call"},
	{Name: CmdDM, Desc: "open a direct-message conversation with a user (switch to DM tab)", Args: []ArgDef{
		{Short: "u", Long: "user", Desc: "username to DM"},
	}},
}

var CommandNames = func() map[string]interface{} {
	m := make(map[string]interface{}, len(Registry))
	for _, c := range Registry {
		m[c.Name] = nil
	}
	return m
}()

// CompletePrefix returns the first command whose name starts with prefix,
// or nil if there is no match. prefix must be at least 2 characters an
// must not contain a space (only the first word is matched).
func CompletePrefix(prefix string) *CommandDef {
	if len(prefix) < 1 || strings.Contains(prefix, " ") {
		return nil
	}
	prefix = strings.ToLower(prefix)
	for i := range Registry {
		if strings.HasPrefix(Registry[i].Name, prefix) {
			return &Registry[i]
		}
	}
	return nil
}

type ArgumentType string

const (
	ArgumentLongForm  = ArgumentType("long")
	ArgumentShortForm = ArgumentType("short")
)

type Argument struct {
	Name   string
	Values []string
	Type   ArgumentType
}

type Command struct {
	Name string
	Args []Argument
}

type CommandParser struct {
	lastTokSeen int
}

func NewParser() *CommandParser {
	return &CommandParser{}
}

// ParseCommand takes a raw input string and parses it into a Command struct.
// A command follows a simple structure:
// [COMMAND_NAME] [ARG1] [ARG2] ... [ARGN]
func (pc *CommandParser) ParseCommand(input string) (Command, error) {
	input = strings.TrimSpace(input)
	i := 0

	commandName := ""
	commandArgs := []*Argument{}

	var previousArg *Argument = nil

	for i < len(input) {
		c := input[i]

		switch c {
		case ' ', '\t', '\n':
			i += 1
			continue
		case '-':
			pc.lastTokSeen = i
			argumentType := ArgumentShortForm
			// check if next is also a '-'
			if i+1 < len(input) && input[i+1] == '-' {
				argumentType = ArgumentLongForm
				pc.lastTokSeen = i + 1
			}

			// possibly get a second - for long form
			argName, err := pc.parseString(input)
			if err != nil {
				return Command{}, err
			}

			arg := Argument{
				Name: argName,
				Type: argumentType,
			}

			// append it to args, set the running arg in case scan in more params for it
			previousArg = &arg
			commandArgs = append(commandArgs, &arg)
			i = pc.lastTokSeen
		default:
			if previousArg != nil {
				pc.lastTokSeen = i - 1
				value, err := pc.parseString(input)
				if err != nil {
					return Command{}, err
				}
				previousArg.Values = append(previousArg.Values, value)
				i = pc.lastTokSeen
			} else if commandName == "" {
				pc.lastTokSeen = i - 1
				name, err := pc.parseString(input)
				if err != nil {
					return Command{}, err
				}
				commandName = strings.ToLower(name)
				if _, ok := CommandNames[commandName]; !ok {
					return Command{}, errors.New("unknown command: " + commandName)
				}
				i = pc.lastTokSeen
			} else {
				return Command{}, errors.New("unexpected token: " + string(c))
			}
		}
	}

	// build the final command
	ret := Command{}
	ret.Name = commandName
	ret.Args = make([]Argument, len(commandArgs))
	for c, arg := range commandArgs {
		ret.Args[c].Name = arg.Name
		ret.Args[c].Type = arg.Type
		ret.Args[c].Values = arg.Values
	}

	return ret, nil

}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n'
}

func (pc *CommandParser) parseString(input string) (string, error) {
	// scan until first whitespace after lastTokSeen
	i := pc.lastTokSeen + 1
	for i < len(input) && !isWhitespace(input[i]) {
		i++
	}

	str := input[pc.lastTokSeen+1 : i]

	pc.lastTokSeen = i
	return str, nil
}
