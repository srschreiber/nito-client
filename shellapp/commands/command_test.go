// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package commands

import (
	"testing"
)

func newParser() *CommandParser {
	return &CommandParser{}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Command
		wantErr bool
	}{
		{
			name:  "simple command no args",
			input: "wcid",
			want:  Command{Name: "wcid", Args: []Argument{}},
		},
		{
			name:  "command with leading/trailing whitespace",
			input: "  clear  ",
			want:  Command{Name: "clear", Args: []Argument{}},
		},
		{
			name:  "command uppercased",
			input: "WCID",
			want:  Command{Name: "wcid", Args: []Argument{}},
		},
		{
			name:  "short flag no value",
			input: "history -n",
			want: Command{
				Name: "history",
				Args: []Argument{
					{Name: "n", Type: ArgumentShortForm},
				},
			},
		},
		{
			name:  "long flag no value",
			input: "history --count",
			want: Command{
				Name: "history",
				Args: []Argument{
					{Name: "count", Type: ArgumentLongForm},
				},
			},
		},
		{
			name:  "short flag with single value",
			input: "history -n 10",
			want: Command{
				Name: "history",
				Args: []Argument{
					{Name: "n", Type: ArgumentShortForm, Values: []string{"10"}},
				},
			},
		},
		{
			name:  "long flag with single value",
			input: "history --count 10",
			want: Command{
				Name: "history",
				Args: []Argument{
					{Name: "count", Type: ArgumentLongForm, Values: []string{"10"}},
				},
			},
		},
		{
			name:  "flag with multiple values",
			input: "history -n foo bar",
			want: Command{
				Name: "history",
				Args: []Argument{
					{Name: "n", Type: ArgumentShortForm, Values: []string{"foo", "bar"}},
				},
			},
		},
		{
			name:  "multiple flags",
			input: "history -n 5 --verbose",
			want: Command{
				Name: "history",
				Args: []Argument{
					{Name: "n", Type: ArgumentShortForm, Values: []string{"5"}},
					{Name: "verbose", Type: ArgumentLongForm},
				},
			},
		},
		{
			name:    "unknown command",
			input:   "unknown",
			wantErr: true,
		},
		{
			name:    "value before any flag",
			input:   "wcid something",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := newParser()
			got, err := pc.ParseCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseCommand(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if len(got.Args) != len(tt.want.Args) {
				t.Fatalf("len(Args) = %d, want %d", len(got.Args), len(tt.want.Args))
			}
			for i, arg := range got.Args {
				wantArg := tt.want.Args[i]
				if arg.Name != wantArg.Name {
					t.Errorf("Args[%d].Name = %q, want %q", i, arg.Name, wantArg.Name)
				}
				if arg.Type != wantArg.Type {
					t.Errorf("Args[%d].Type = %q, want %q", i, arg.Type, wantArg.Type)
				}
				if len(arg.Values) != len(wantArg.Values) {
					t.Fatalf("Args[%d] len(Values) = %d, want %d", i, len(arg.Values), len(wantArg.Values))
				}
				for j, v := range arg.Values {
					if v != wantArg.Values[j] {
						t.Errorf("Args[%d].Values[%d] = %q, want %q", i, j, v, wantArg.Values[j])
					}
				}
			}
		})
	}
}
