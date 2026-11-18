// Package vehicle identifies the make and model year from a VIN.
package vehicle

import (
	"strings"
	"time"
)

// Group is a family of makes sharing diagnostic conventions.
type Group string

const (
	GroupGeneric Group = "Generic"
	GroupVAG     Group = "VAG"
	GroupBMW     Group = "BMW"
	GroupHonda   Group = "Honda"
)

// Groups lists the selectable profiles, in cycling order.
var Groups = []Group{GroupVAG, GroupBMW, GroupHonda, GroupGeneric}

// Make is a decoded manufacturer.
type Make struct {
	Name  string // "Skoda", "BMW", …; "" if unknown
	Group Group
}

// World manufacturer identifiers (first 3 VIN characters).
var wmi = map[string]Make{
	// VAG
	"TMB": {"Skoda", GroupVAG}, "TMP": {"Skoda", GroupVAG},
	"WVW": {"Volkswagen", GroupVAG}, "WVG": {"Volkswagen", GroupVAG}, "WV1": {"Volkswagen", GroupVAG},
	"WV2": {"Volkswagen", GroupVAG}, "1VW": {"Volkswagen", GroupVAG}, "3VW": {"Volkswagen", GroupVAG},
	"9BW": {"Volkswagen", GroupVAG},
	"WAU": {"Audi", GroupVAG}, "WA1": {"Audi", GroupVAG}, "TRU": {"Audi", GroupVAG}, "WUA": {"Audi", GroupVAG},
	"VSS": {"SEAT / Cupra", GroupVAG},
	// BMW group
	"WBA": {"BMW", GroupBMW}, "WBS": {"BMW M", GroupBMW}, "WBY": {"BMW i", GroupBMW},
	"4US": {"BMW", GroupBMW}, "5UX": {"BMW", GroupBMW}, "5UM": {"BMW M", GroupBMW}, "5YM": {"BMW M", GroupBMW},
	"WMW": {"MINI", GroupBMW},
	// Honda / Acura
	"JHM": {"Honda", GroupHonda}, "JHL": {"Honda", GroupHonda}, "SHH": {"Honda", GroupHonda},
	"SHS": {"Honda", GroupHonda}, "1HG": {"Honda", GroupHonda}, "2HG": {"Honda", GroupHonda},
	"5FN": {"Honda", GroupHonda}, "5J6": {"Honda", GroupHonda}, "2HK": {"Honda", GroupHonda},
	"JH4": {"Acura", GroupHonda}, "19U": {"Acura", GroupHonda},
	// Common others (generic profile)
	"WDD": {"Mercedes-Benz", GroupGeneric}, "WDB": {"Mercedes-Benz", GroupGeneric},
	"WF0": {"Ford", GroupGeneric}, "W0L": {"Opel", GroupGeneric}, "VF1": {"Renault", GroupGeneric},
	"VF3": {"Peugeot", GroupGeneric}, "VF7": {"Citroën", GroupGeneric}, "YV1": {"Volvo", GroupGeneric},
	"KMH": {"Hyundai", GroupGeneric}, "KNA": {"Kia", GroupGeneric}, "JN1": {"Nissan", GroupGeneric},
	"SJN": {"Nissan", GroupGeneric}, "JM1": {"Mazda", GroupGeneric}, "JF1": {"Subaru", GroupGeneric},
	"ZFA": {"Fiat", GroupGeneric}, "SAL": {"Land Rover", GroupGeneric}, "SAJ": {"Jaguar", GroupGeneric},
}

// Decode returns the make and model year (0 if unknown) for a VIN.
func Decode(vin string) (Make, int) {
	vin = strings.ToUpper(strings.TrimSpace(vin))
	if len(vin) < 3 {
		return Make{Group: GroupGeneric}, 0
	}
	mk, ok := wmi[vin[:3]]
	if !ok && strings.HasPrefix(vin, "JT") {
		mk, ok = Make{"Toyota", GroupGeneric}, true
	}
	if !ok {
		mk = Make{Group: GroupGeneric}
	}
	year := 0
	if len(vin) == 17 {
		year = modelYear(vin[9])
	}
	return mk, year
}

// modelYear decodes VIN position 10. The code repeats every 30 years, so
// the most recent year not in the future is chosen.
func modelYear(c byte) int {
	const codes = "ABCDEFGHJKLMNPRSTVWXY123456789"
	i := strings.IndexByte(codes, c)
	if i < 0 {
		return 0
	}
	y := 2010 + i
	for y > time.Now().Year()+1 {
		y -= 30
	}
	return y
}
