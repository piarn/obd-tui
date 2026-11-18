package obd

import (
	"fmt"
	"strconv"
)

// Describe returns a human description for a DTC. Generic SAE codes get an
// exact text when known, otherwise a category; manufacturer codes say so.
func Describe(code string) string {
	if d, ok := dtcText[code]; ok {
		return d
	}
	if len(code) != 5 {
		return "Unknown code"
	}
	switch code[0] {
	case 'P':
		if d, err := strconv.Atoi(code[1:]); err == nil {
			switch {
			case d >= 301 && d <= 312:
				return fmt.Sprintf("Cylinder %d misfire detected", d-300)
			case d >= 201 && d <= 212:
				return fmt.Sprintf("Injector circuit malfunction – cylinder %d", d-200)
			case d >= 351 && d <= 362:
				return fmt.Sprintf("Ignition coil %c primary/secondary circuit", 'A'+d-351)
			}
		}
		switch code[1] {
		case '1', '3':
			return "Manufacturer-specific powertrain code"
		}
		switch code[1:3] {
		case "00", "01", "02":
			return "Fuel and air metering (generic)"
		case "03":
			return "Ignition system or misfire (generic)"
		case "04":
			return "Auxiliary emission controls (generic)"
		case "05":
			return "Vehicle speed, idle control or auxiliary inputs (generic)"
		case "06":
			return "Computer / output circuit (generic)"
		case "07", "08", "09":
			return "Transmission (generic)"
		case "0A":
			return "Hybrid propulsion (generic)"
		case "20", "21", "22":
			return "Fuel and air metering (generic)"
		case "23":
			return "Ignition system (generic)"
		case "24", "25", "26":
			return "Emission / auxiliary controls (generic)"
		case "27", "28", "29":
			return "Transmission (generic)"
		}
		return "Generic powertrain code"
	case 'C':
		return "Chassis code (ABS / suspension / steering)"
	case 'B':
		return "Body code (airbags / comfort / lighting)"
	case 'U':
		return "Network communication code"
	}
	return "Unknown code"
}

var dtcText = map[string]string{
	"P0010": "Intake camshaft position actuator circuit (bank 1)",
	"P0011": "Intake camshaft timing over-advanced (bank 1)",
	"P0012": "Intake camshaft timing over-retarded (bank 1)",
	"P0013": "Exhaust camshaft position actuator circuit (bank 1)",
	"P0014": "Exhaust camshaft timing over-advanced (bank 1)",
	"P0016": "Crankshaft/camshaft position correlation (bank 1 sensor A)",
	"P0017": "Crankshaft/camshaft position correlation (bank 1 sensor B)",
	"P0020": "Intake camshaft position actuator circuit (bank 2)",
	"P0021": "Intake camshaft timing over-advanced (bank 2)",
	"P0030": "HO2S heater control circuit (bank 1 sensor 1)",
	"P0031": "HO2S heater circuit low (bank 1 sensor 1)",
	"P0032": "HO2S heater circuit high (bank 1 sensor 1)",
	"P0036": "HO2S heater control circuit (bank 1 sensor 2)",
	"P0037": "HO2S heater circuit low (bank 1 sensor 2)",
	"P0038": "HO2S heater circuit high (bank 1 sensor 2)",
	"P0040": "O2 sensor signals swapped bank 1/bank 2 sensor 1",
	"P0068": "MAP/MAF – throttle position correlation",
	"P0087": "Fuel rail/system pressure too low",
	"P0088": "Fuel rail/system pressure too high",
	"P0089": "Fuel pressure regulator performance",
	"P0093": "Fuel system leak detected – large leak",
	"P0100": "Mass air flow circuit malfunction",
	"P0101": "Mass air flow circuit range/performance",
	"P0102": "Mass air flow circuit low input",
	"P0103": "Mass air flow circuit high input",
	"P0104": "Mass air flow circuit intermittent",
	"P0105": "MAP/barometric pressure circuit malfunction",
	"P0106": "MAP/barometric pressure circuit range/performance",
	"P0107": "MAP/barometric pressure circuit low input",
	"P0108": "MAP/barometric pressure circuit high input",
	"P0110": "Intake air temperature circuit malfunction",
	"P0111": "Intake air temperature circuit range/performance",
	"P0112": "Intake air temperature circuit low input",
	"P0113": "Intake air temperature circuit high input",
	"P0115": "Engine coolant temperature circuit malfunction",
	"P0116": "Engine coolant temperature circuit range/performance",
	"P0117": "Engine coolant temperature circuit low input",
	"P0118": "Engine coolant temperature circuit high input",
	"P0120": "Throttle/pedal position sensor A circuit malfunction",
	"P0121": "Throttle/pedal position sensor A range/performance",
	"P0122": "Throttle/pedal position sensor A circuit low input",
	"P0123": "Throttle/pedal position sensor A circuit high input",
	"P0125": "Insufficient coolant temperature for closed-loop fuel control",
	"P0128": "Coolant thermostat (coolant temp below regulating temperature)",
	"P0130": "O2 sensor circuit malfunction (bank 1 sensor 1)",
	"P0131": "O2 sensor circuit low voltage (bank 1 sensor 1)",
	"P0132": "O2 sensor circuit high voltage (bank 1 sensor 1)",
	"P0133": "O2 sensor circuit slow response (bank 1 sensor 1)",
	"P0134": "O2 sensor circuit no activity detected (bank 1 sensor 1)",
	"P0135": "O2 sensor heater circuit malfunction (bank 1 sensor 1)",
	"P0136": "O2 sensor circuit malfunction (bank 1 sensor 2)",
	"P0137": "O2 sensor circuit low voltage (bank 1 sensor 2)",
	"P0138": "O2 sensor circuit high voltage (bank 1 sensor 2)",
	"P0139": "O2 sensor circuit slow response (bank 1 sensor 2)",
	"P0140": "O2 sensor circuit no activity detected (bank 1 sensor 2)",
	"P0141": "O2 sensor heater circuit malfunction (bank 1 sensor 2)",
	"P0150": "O2 sensor circuit malfunction (bank 2 sensor 1)",
	"P0151": "O2 sensor circuit low voltage (bank 2 sensor 1)",
	"P0152": "O2 sensor circuit high voltage (bank 2 sensor 1)",
	"P0153": "O2 sensor circuit slow response (bank 2 sensor 1)",
	"P0155": "O2 sensor heater circuit malfunction (bank 2 sensor 1)",
	"P0156": "O2 sensor circuit malfunction (bank 2 sensor 2)",
	"P0161": "O2 sensor heater circuit malfunction (bank 2 sensor 2)",
	"P0171": "System too lean (bank 1)",
	"P0172": "System too rich (bank 1)",
	"P0174": "System too lean (bank 2)",
	"P0175": "System too rich (bank 2)",
	"P0181": "Fuel temperature sensor A circuit range/performance",
	"P0190": "Fuel rail pressure sensor circuit malfunction",
	"P0191": "Fuel rail pressure sensor circuit range/performance",
	"P0192": "Fuel rail pressure sensor circuit low input",
	"P0193": "Fuel rail pressure sensor circuit high input",
	"P0200": "Injector circuit malfunction",
	"P0217": "Engine over-temperature condition",
	"P0219": "Engine over-speed condition",
	"P0220": "Throttle/pedal position sensor B circuit malfunction",
	"P0221": "Throttle/pedal position sensor B range/performance",
	"P0222": "Throttle/pedal position sensor B circuit low input",
	"P0223": "Throttle/pedal position sensor B circuit high input",
	"P0230": "Fuel pump primary circuit malfunction",
	"P0234": "Turbo/supercharger overboost condition",
	"P0235": "Turbo/supercharger boost sensor A circuit",
	"P0236": "Turbo/supercharger boost sensor A range/performance",
	"P0237": "Turbo/supercharger boost sensor A circuit low",
	"P0238": "Turbo/supercharger boost sensor A circuit high",
	"P0243": "Turbo/supercharger wastegate solenoid A",
	"P0261": "Cylinder 1 injector circuit low",
	"P0299": "Turbo/supercharger underboost condition",
	"P0300": "Random/multiple cylinder misfire detected",
	"P0313": "Misfire detected with low fuel",
	"P0316": "Misfire detected on startup (first 1000 revolutions)",
	"P0320": "Ignition/distributor engine speed input circuit",
	"P0325": "Knock sensor 1 circuit malfunction (bank 1)",
	"P0326": "Knock sensor 1 circuit range/performance (bank 1)",
	"P0327": "Knock sensor 1 circuit low input (bank 1)",
	"P0328": "Knock sensor 1 circuit high input (bank 1)",
	"P0330": "Knock sensor 2 circuit malfunction (bank 2)",
	"P0335": "Crankshaft position sensor A circuit malfunction",
	"P0336": "Crankshaft position sensor A range/performance",
	"P0340": "Camshaft position sensor A circuit malfunction (bank 1)",
	"P0341": "Camshaft position sensor A range/performance (bank 1)",
	"P0345": "Camshaft position sensor A circuit (bank 2)",
	"P0365": "Camshaft position sensor B circuit (bank 1)",
	"P0380": "Glow plug/heater circuit A malfunction",
	"P0400": "Exhaust gas recirculation flow malfunction",
	"P0401": "Exhaust gas recirculation flow insufficient",
	"P0402": "Exhaust gas recirculation flow excessive",
	"P0403": "Exhaust gas recirculation control circuit",
	"P0404": "Exhaust gas recirculation control circuit range/performance",
	"P0405": "EGR sensor A circuit low",
	"P0406": "EGR sensor A circuit high",
	"P0410": "Secondary air injection system malfunction",
	"P0411": "Secondary air injection system incorrect flow",
	"P0420": "Catalyst system efficiency below threshold (bank 1)",
	"P0421": "Warm-up catalyst efficiency below threshold (bank 1)",
	"P0430": "Catalyst system efficiency below threshold (bank 2)",
	"P0440": "Evaporative emission system malfunction",
	"P0441": "Evaporative emission system incorrect purge flow",
	"P0442": "Evaporative emission system leak detected (small leak)",
	"P0443": "Evaporative emission purge control valve circuit",
	"P0446": "Evaporative emission vent control circuit",
	"P0449": "Evaporative emission vent valve/solenoid circuit",
	"P0451": "Evaporative emission pressure sensor range/performance",
	"P0452": "Evaporative emission pressure sensor low input",
	"P0453": "Evaporative emission pressure sensor high input",
	"P0455": "Evaporative emission system leak detected (large leak)",
	"P0456": "Evaporative emission system leak detected (very small leak)",
	"P0457": "Evaporative emission leak detected (fuel cap loose/off)",
	"P0460": "Fuel level sensor A circuit malfunction",
	"P0461": "Fuel level sensor A circuit range/performance",
	"P0462": "Fuel level sensor A circuit low",
	"P0463": "Fuel level sensor A circuit high",
	"P0480": "Cooling fan 1 control circuit",
	"P0481": "Cooling fan 2 control circuit",
	"P0496": "Evaporative emission system high purge flow",
	"P0497": "Evaporative emission system low purge flow",
	"P0500": "Vehicle speed sensor A malfunction",
	"P0501": "Vehicle speed sensor A range/performance",
	"P0505": "Idle air control system malfunction",
	"P0506": "Idle control system RPM lower than expected",
	"P0507": "Idle control system RPM higher than expected",
	"P0520": "Engine oil pressure sensor/switch circuit",
	"P0521": "Engine oil pressure sensor/switch range/performance",
	"P0530": "A/C refrigerant pressure sensor A circuit",
	"P0560": "System voltage malfunction",
	"P0562": "System voltage low",
	"P0563": "System voltage high",
	"P0571": "Brake switch A circuit",
	"P0600": "Serial communication link malfunction",
	"P0601": "Internal control module memory checksum error",
	"P0602": "Control module programming error",
	"P0603": "Internal control module keep-alive memory (KAM) error",
	"P0604": "Internal control module RAM error",
	"P0605": "Internal control module ROM error",
	"P0606": "Control module processor fault",
	"P0607": "Control module performance",
	"P0620": "Generator control circuit",
	"P0627": "Fuel pump A control circuit open",
	"P0641": "Sensor reference voltage A circuit open",
	"P0651": "Sensor reference voltage B circuit open",
	"P0670": "Glow plug control module circuit",
	"P0700": "Transmission control system malfunction (TCM has a code)",
	"P0705": "Transmission range sensor circuit (PRNDL input)",
	"P0710": "Transmission fluid temperature sensor A circuit",
	"P0715": "Input/turbine speed sensor A circuit",
	"P0720": "Output speed sensor circuit",
	"P0730": "Incorrect gear ratio",
	"P0740": "Torque converter clutch solenoid circuit",
	"P0741": "Torque converter clutch circuit performance or stuck off",
	"P0750": "Shift solenoid A",
	"P0755": "Shift solenoid B",
	"P0760": "Shift solenoid C",
	"P0765": "Shift solenoid D",
	"P0776": "Pressure control solenoid B performance or stuck off",
	"P0800": "Transfer case control system (MIL request)",
	"P0830": "Clutch pedal switch A circuit",
	"P2002": "Diesel particulate filter efficiency below threshold (bank 1)",
	"P2004": "Intake manifold runner control stuck open (bank 1)",
	"P2096": "Post-catalyst fuel trim system too lean (bank 1)",
	"P2097": "Post-catalyst fuel trim system too rich (bank 1)",
	"P2101": "Throttle actuator control motor circuit range/performance",
	"P2135": "Throttle/pedal position sensor A/B voltage correlation",
	"P2138": "Throttle/pedal position sensor D/E voltage correlation",
	"P2181": "Cooling system performance",
	"P2187": "System too lean at idle (bank 1)",
	"P2188": "System too rich at idle (bank 1)",
	"P2195": "O2 sensor signal stuck lean (bank 1 sensor 1)",
	"P2196": "O2 sensor signal stuck rich (bank 1 sensor 1)",
	"P2270": "O2 sensor signal stuck lean (bank 1 sensor 2)",
	"P2271": "O2 sensor signal stuck rich (bank 1 sensor 2)",
	"P2279": "Intake air system leak",
	"P242F": "Diesel particulate filter restriction – ash accumulation",
	"P2452": "Diesel particulate filter pressure sensor A circuit",
	"P2453": "Diesel particulate filter pressure sensor A range/performance",
	"P2463": "Diesel particulate filter restriction – soot accumulation",
	"P2BAD": "NOx exceedance – root cause unknown",
	"U0001": "High speed CAN communication bus",
	"U0073": "Control module communication bus A off",
	"U0100": "Lost communication with ECM/PCM A",
	"U0101": "Lost communication with TCM",
	"U0121": "Lost communication with ABS control module",
	"U0140": "Lost communication with body control module",
	"U0155": "Lost communication with instrument panel cluster",
}
