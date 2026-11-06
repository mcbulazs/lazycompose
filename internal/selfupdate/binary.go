package selfupdate

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// osNames are used in messages.
var osNames = map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}

// checkBinary makes sure bin is a program for goos and goarch, by reading
// the machine type from its header: ELF on Linux, Mach-O on macOS and PE on
// Windows.
func checkBinary(bin []byte, goos, goarch string) error {
	machine, ok := machineOf(bin, goos)
	if !ok {
		return fmt.Errorf("the downloaded file is not a %s program, not installing it", osNames[goos])
	}
	if machine != goarch {
		return fmt.Errorf("the downloaded program is for %s, not %s, not installing it", machine, goarch)
	}
	return nil
}

// machineOf returns the GOARCH a binary was built for, if it is a binary
// for goos at all.
func machineOf(bin []byte, goos string) (string, bool) {
	le := binary.LittleEndian
	switch goos {
	case "linux":
		// e_machine is at offset 18 of the ELF header
		if len(bin) < 20 || !bytes.HasPrefix(bin, []byte("\x7fELF")) {
			return "", false
		}
		return arch(map[uint32]string{62: "amd64", 183: "arm64"}, uint32(le.Uint16(bin[18:])))
	case "darwin":
		// 64-bit Mach-O: the magic, then the CPU type
		if len(bin) < 8 || le.Uint32(bin) != 0xfeedfacf {
			return "", false
		}
		return arch(map[uint32]string{0x01000007: "amd64", 0x0100000c: "arm64"}, le.Uint32(bin[4:]))
	case "windows":
		// "MZ", the offset of the PE header at 0x3c, then "PE\0\0" and the machine
		if len(bin) < 0x40 || !bytes.HasPrefix(bin, []byte("MZ")) {
			return "", false
		}
		pe := int(le.Uint32(bin[0x3c:]))
		if pe < 0x40 || pe+6 > len(bin) || !bytes.Equal(bin[pe:pe+4], []byte("PE\x00\x00")) {
			return "", false
		}
		return arch(map[uint32]string{0x8664: "amd64", 0xaa64: "arm64"}, uint32(le.Uint16(bin[pe+4:])))
	}
	return "", false
}

func arch(known map[uint32]string, machine uint32) (string, bool) {
	if a, ok := known[machine]; ok {
		return a, true
	}
	return fmt.Sprintf("machine type %#x", machine), true
}
