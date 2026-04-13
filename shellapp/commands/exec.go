// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	CmdClear       = "clear"
	CmdExit        = "exit"
	CmdHistory     = "history"
	CmdEcho        = "echo"
	CmdRegister    = "register"
	CmdLogin       = "login"
	CmdPing        = "ping"
	CmdWcid        = "wcid"
	CmdRoomCreate  = "room-create"
	CmdRoomList    = "room-list"
	CmdRoomSelect  = "room-select"
	CmdRoomInvite  = "room-invite"
	CmdRoomInvites = "room-invites"
	CmdRoomAccept  = "room-accept"
	CmdSay         = "say"
	CmdJump        = "jump"
	CmdVoiceJoin   = "voice-join"
	CmdVoiceLeave  = "voice-leave"
	CmdDM          = "dm"
)

// JumpLine holds the target line requested by the most recent jump command.
var JumpLine int

var parser = NewParser()

func extractArg(args []Argument, short, long string) string {
	for _, a := range args {
		if a.Name == short || a.Name == long {
			if len(a.Values) > 0 {
				return a.Values[0]
			}
		}
	}
	return ""
}

// extractArgValues returns all values for a flag, supporting multi-word inputs
// like `echo -m hello world` where Values = ["hello", "world"].
func extractArgValues(args []Argument, short, long string) []string {
	for _, a := range args {
		if a.Name == short || a.Name == long {
			return a.Values
		}
	}
	return nil
}

func wcid(args []Argument) string {
	// check for -c/--command filter
	filter := ""
	for _, a := range args {
		if a.Name == "c" || a.Name == "command" {
			if len(a.Values) > 0 {
				filter = strings.ToLower(a.Values[0])
			}
		}
	}

	var lines []string
	for _, cmd := range Registry {
		if filter != "" && cmd.Name != filter {
			continue
		}
		// first, replace all newlines in description with newline + tab
		cmd.Desc = strings.ReplaceAll(cmd.Desc, "\n", "\n\t")
		line := fmt.Sprintf("%s\n\t%s", cmd.Name, cmd.Desc)
		lines = append(lines, line)
		for _, arg := range cmd.Args {
			var flag string
			switch {
			case arg.Short != "" && arg.Long != "":
				flag = fmt.Sprintf("-%s / --%s", arg.Short, arg.Long)
			case arg.Long != "":
				flag = "--" + arg.Long
			default:
				flag = "-" + arg.Short
			}
			lines = append(lines, fmt.Sprintf("\t%s  %s", flag, arg.Desc))
		}
		// add newline separator between commands
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		return "unknown command: " + filter
	}
	return strings.Join(lines, "\n")
}

// ExecCommand takes a raw command string, parses it, and executes the corresponding action.
// Returns:
// - output: the string output to display to the user (if any)
// - signal: an optional status code (e.g., for success/failure)
// - error: any error that occurred during command execution
func ExecCommand(cmd string) (string, Signal, error) {
	parsedCommand, err := parser.ParseCommand(cmd)
	if err != nil {
		return "", 0, err
	}

	switch strings.ToLower(parsedCommand.Name) {
	case CmdClear:
		return "", SignalClear, nil
	case CmdExit:
		return "Exiting the shell...", SignalExit, nil
	case CmdHistory:
		return "Command history is not implemented yet.", SignalNone, nil
	case CmdEcho:
		out, err := echoCmd(parsedCommand.Args)
		return out, SignalNone, err
	case CmdRegister:
		sig, err := registerCmd(parsedCommand.Args)
		return "", sig, err
	case CmdLogin:
		sig, err := loginCmd(parsedCommand.Args)
		return "", sig, err
	case CmdPing:
		out, err := ping(parsedCommand.Args)
		return out, SignalNone, err
	case CmdWcid:
		return wcid(parsedCommand.Args), SignalNone, nil
	case CmdRoomCreate:
		out, err := roomCreateCmd(parsedCommand.Args)
		if err != nil {
			return "", SignalNone, err
		}
		return out, SignalRefreshRooms, nil
	case CmdRoomList:
		out, err := roomListCmd()
		return out, SignalNone, err
	case CmdRoomSelect:
		return roomSelectCmd(parsedCommand.Args)
	case CmdRoomInvite:
		out, err := roomInviteCmd(parsedCommand.Args)
		return out, SignalNone, err
	case CmdRoomInvites:
		out, err := roomInvitesCmd()
		return out, SignalNone, err
	case CmdRoomAccept:
		out, err := roomAcceptCmd(parsedCommand.Args)
		if err != nil {
			return "", SignalNone, err
		}
		return out, SignalRefreshRooms, nil
	case CmdSay:
		out, err := sayCmd(parsedCommand.Args)
		return out, SignalNone, err
	case CmdJump:
		lineStr := extractArg(parsedCommand.Args, "L", "line")
		if lineStr == "" {
			return "", SignalNone, errors.New("jump: -L <line> is required")
		}
		n, err := strconv.Atoi(lineStr)
		if err != nil {
			return "", SignalNone, errors.New("jump: -L must be an integer")
		}
		JumpLine = n
		return "", SignalJump, nil
	case CmdVoiceJoin:
		out, err := voiceJoinCmd()
		return out, SignalNone, err
	case CmdVoiceLeave:
		out, err := voiceLeaveCmd()
		if err != nil {
			return out, SignalNone, err
		}
		return out, SignalVoiceLeave, nil
	case CmdDM:
		user := extractArg(parsedCommand.Args, "u", "user")
		if user == "" {
			return "", SignalNone, errors.New("dm: -u/--user <username> is required")
		}
		DMUser = user
		return "", SignalStartDM, nil
	default:
		return "", SignalNone, errors.New("unknown command: " + parsedCommand.Name)
	}
}
