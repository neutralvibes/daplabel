package discovery

import (
	"reflect"
	"testing"
)

func TestExpandCommaSeparated_noCommaPassthrough(t *testing.T) {
	got := ExpandCommaSeparated([]string{".", "./project", "/absolute/path"})
	want := []string{".", "./project", "/absolute/path"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandCommaSeparated_basicSplit(t *testing.T) {
	got := ExpandCommaSeparated([]string{"./a,./b,./c"})
	want := []string{"./a", "./b", "./c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandCommaSeparated_spacesAroundComma(t *testing.T) {
	got := ExpandCommaSeparated([]string{"./a ,  ./b ,./c"})
	want := []string{"./a", "./b", "./c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandCommaSeparated_trailingComma(t *testing.T) {
	got := ExpandCommaSeparated([]string{"./a,./b,"})
	want := []string{"./a", "./b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandCommaSeparated_multipleArgsWithAndWithoutCommas(t *testing.T) {
	got := ExpandCommaSeparated([]string{"./single", "./a,./b", "./c"})
	want := []string{"./single", "./a", "./b", "./c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandCommaSeparated_emptyInput(t *testing.T) {
	got := ExpandCommaSeparated(nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
	got = ExpandCommaSeparated([]string{})
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
