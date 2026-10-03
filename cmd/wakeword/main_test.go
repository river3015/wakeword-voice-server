package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestStringListCollectsRepeatedFlags(t *testing.T) {
	var got stringList
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Var(&got, "request-wav", "")

	if err := flags.Parse([]string{
		"--request-wav", "first.wav",
		"--request-wav", "second.wav",
	}); err != nil {
		t.Fatal(err)
	}

	want := stringList{"first.wav", "second.wav"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request WAVs = %v, want %v", got, want)
	}
	if got.String() != "first.wav,second.wav" {
		t.Fatalf("String() = %q", got.String())
	}
}
