package prompt

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Response
	}{
		{"lowercase y", "y\n", Yes},
		{"uppercase Y", "Y\n", Yes},
		{"lowercase n", "n\n", No},
		{"uppercase N", "N\n", No},
		{"lowercase q", "q\n", Quit},
		{"uppercase Q", "Q\n", Quit},
		{"lowercase a", "a\n", All},
		{"uppercase A", "A\n", All},
		{"empty line defaults to No", "\n", No},
		{"whitespace-only line defaults to No", "   \n", No},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got := Confirm(&out, strings.NewReader(tc.input), "Proceed?")
			if got != tc.want {
				t.Errorf("Confirm(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(out.String(), "Proceed?") {
				t.Errorf("expected the prompt message in output, got %q", out.String())
			}
		})
	}
}

func TestConfirm_reprompsOnUnrecognisedInputThenAccepts(t *testing.T) {
	var out bytes.Buffer
	got := Confirm(&out, strings.NewReader("banana\ny\n"), "Proceed?")
	if got != Yes {
		t.Errorf("Confirm = %v, want Yes after reprompt", got)
	}
	if !strings.Contains(out.String(), "Please answer Y(es), N(o), A(ll), or Q(uit).") {
		t.Errorf("expected a reprompt message, got %q", out.String())
	}
}

func TestConfirm_closedInputWithNoAnswerIsQuit(t *testing.T) {
	var out bytes.Buffer
	got := Confirm(&out, strings.NewReader(""), "Proceed?")
	if got != Quit {
		t.Errorf("Confirm on closed/empty input = %v, want Quit", got)
	}
}
