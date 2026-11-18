package elm

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseCANSingleFrameMultiECU(t *testing.T) {
	lines := CleanLines("0100\rSEARCHING...\r7E8064100BE3EB813\r7E906410098180000\r\r>", "0100")
	r, err := Parse(lines, Format{Headers: true, Protocol: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].ECU != "7E8" || r[1].ECU != "7E9" {
		t.Fatalf("got %+v", r)
	}
	if !bytes.Equal(r[0].Data, []byte{0x41, 0x00, 0xBE, 0x3E, 0xB8, 0x13}) {
		t.Fatalf("data %X", r[0].Data)
	}
}

func TestParseCANMultiFrameWithPadding(t *testing.T) {
	lines := []string{
		"7E8 10 14 49 02 01 57 56 57",
		"7E8 21 5A 5A 5A 31 4B 5A 41",
		"7E8 22 57 31 32 33 34 35 36",
	}
	r, err := Parse(lines, Format{Headers: true, Protocol: 6})
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x49, 0x02, 0x01}, "WVWZZZ1KZAW123456"...)
	if len(r) != 1 || !bytes.Equal(r[0].Data, want) {
		t.Fatalf("got %X want %X", r[0].Data, want)
	}
}

func TestParseCAN29Bit(t *testing.T) {
	r, err := Parse([]string{"18DAF110034105 5BAAAAAAAA"}, Format{Headers: true, Protocol: 7})
	if err != nil {
		t.Fatal(err)
	}
	if r[0].ECU != "18DAF110" || !bytes.Equal(r[0].Data, []byte{0x41, 0x05, 0x5B}) {
		t.Fatalf("got %+v", r)
	}
}

func TestParseSegmentedHeadersOff(t *testing.T) {
	lines := []string{"014", "0: 49 02 01 57 56 57", "1: 5A 5A 5A 31 4B 5A 41", "2: 57 31 32 33 34 35 36"}
	r, err := Parse(lines, Format{Protocol: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(r[0].Data) != 20 || string(r[0].Data[3:]) != "WVWZZZ1KZAW123456" {
		t.Fatalf("got %X", r[0].Data)
	}
}

func TestParseLegacyHeaders(t *testing.T) {
	r, err := Parse([]string{"48 6B 10 41 0C 1A F8 C4"}, Format{Headers: true, Protocol: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r[0].ECU != "10" || !bytes.Equal(r[0].Data, []byte{0x41, 0x0C, 0x1A, 0xF8}) {
		t.Fatalf("got %+v", r)
	}
}

func TestParseErrors(t *testing.T) {
	for raw, want := range map[string]Error{
		"NO DATA":                         ErrNoData,
		"SEARCHING...\rUNABLE TO CONNECT": ErrUnableToConnect,
		"?":                               ErrUnknownCmd,
		"CAN ERROR":                       ErrCAN,
	} {
		_, err := Parse(CleanLines(raw, "0100"), Format{Headers: true, Protocol: 6})
		if !errors.Is(err, want) {
			t.Errorf("%q: got %v want %v", raw, err, want)
		}
	}
}

func TestNegativeResponseKept(t *testing.T) {
	r, err := Parse([]string{"77D037F1922AAAAAAAA"}, Format{Headers: true, Protocol: 6})
	if err != nil || r[0].ECU != "77D" || !bytes.Equal(r[0].Data, []byte{0x7F, 0x19, 0x22}) {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestParseExtendedAddressing(t *testing.T) {
	// BMW: DME at 0x12 answers on 0x612, first byte F1 = tester address.
	lines := []string{"612 F1 10 0B 59 02 FF 2A 82", "612 F1 21 00 08 2A 83 00 09"}
	r, err := Parse(lines, Format{Headers: true, Protocol: 6, ExtAddr: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x59, 0x02, 0xFF, 0x2A, 0x82, 0x00, 0x08, 0x2A, 0x83, 0x00, 0x09}
	if r[0].ECU != "612" || !bytes.Equal(r[0].Data, want) {
		t.Fatalf("got %X", r[0].Data)
	}
}
