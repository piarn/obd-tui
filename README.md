# lazyobd

Terminal UI for ELM327 USB OBD-II cables: live sensor dashboard, trouble codes, a module scan (ABS, airbag, body, cluster… over UDS), vehicle info and readiness monitors, CSV logging, and a raw command console.

```sh
go build -o lazyobd .
./lazyobd --sim          # try it without a car (built-in fake ELM327)
./lazyobd                # auto-detect the cable
./lazyobd --probe        # no TUI: print raw adapter/vehicle responses
./lazyobd --port /dev/ttyUSB0 --baud 38400 --log drive.csv
```

## First time with the cable

1. Plug it in and check what showed up: `ls -l /dev/serial/by-id/ /dev/ttyUSB*` and `dmesg | tail`.
2. The device belongs to a group, usually `uucp` on Gentoo or `dialout` elsewhere. Add yourself to it with `sudo usermod -aG uucp $USER`, then log out and back in.
3. Turn the ignition on and run `./lazyobd --probe`. If it can't find the baud rate, try `--baud 9600` or `--baud 115200`.

## Module scan (tab 3)

The make comes from the VIN (`TMB…` Skoda, `WBA…` BMW, `JHM…`/`SHH…` Honda, …) and picks a profile listing which modules to ask. `m` cycles the profile by hand, for cars that don't report a VIN.

- **VAG (Skoda/VW/Audi/SEAT):** engine, gearbox, gateway, ABS, cluster, airbag, central electronics, steering and HVAC on their UDS addresses (request ID + 0x6A).
- **BMW:** DME, EGS, DSC, airbag, ZGW, CAS/FEM, KOMBI and IHKA, reached through extended addressing on 0x6F1. This needs an adapter that supports `ATCEA`. Older E-series modules that only speak KWP2000 are read with service 18 instead.
- **Honda / other makes:** the standard OBD addresses `7E0`–`7E7`. `D` runs a deep scan that sweeps every 29-bit `18DAxxF1` address (about a minute) to find whatever else answers.

Codes show with their failure type (e.g. `C0035-1C`, circuit voltage out of range) and status (active / intermittent / confirmed / pending). `c` clears the selected module and `C` clears every module with faults; both ask first. If a module refuses, the clear is retried in the extended diagnostic session.

Cheap ELM327 clones often lack `ATCRA`/`ATFCSH`/`ATCEA`. When a command is missing, the scan stops with a clear error and doesn't guess. An STN-based adapter (OBDLink SX/EX) supports all of them.

## Keys

Vim-style. Press `?` in the app for the full list.

| | |
|---|---|
| `H`/`L`, `tab`, `gt`/`gT`, `1-5` | switch tabs |
| `h j k l`, `gg`/`G`, `C-d`/`C-u` | move |
| `/` | filter (dashboard, codes) or search (console), `n`/`N` next/prev |
| `enter` | full-screen graph of the selected sensor |
| `space` | pause live polling |
| `w` | start/stop CSV recording |
| `r` / `c` | refresh / clear trouble codes (asks to confirm) |
| `i`, `esc` | console insert / normal mode, `C-p`/`C-n` history |
| `s` / `D` / `m` / `a` | module scan / deep scan / cycle make profile / show absent modules |
| `R` | reconnect |
| `q` | quit |
