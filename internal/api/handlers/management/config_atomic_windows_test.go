//go:build windows

package management

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWriteConfigAtomicInvokesWindowsSecurityHelpersBeforeReplace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "managed")
	path := filepath.Join(dir, "config.yaml")
	directorySecured := false
	fileSecured := false
	replaced := false

	errWrite := writeConfigAtomicWithSecurity(
		path,
		[]byte("debug: true\n"),
		func(source, destination string) error {
			if !directorySecured || !fileSecured {
				t.Fatal("replace called before Windows security helpers")
			}
			replaced = true
			return os.Rename(source, destination)
		},
		func(securedPath string) error {
			if errMkdir := os.MkdirAll(securedPath, 0o700); errMkdir != nil {
				t.Fatalf("create secured directory: %v", errMkdir)
			}
			directorySecured = true
			if securedPath != dir {
				t.Fatalf("secured directory = %q, want %q", securedPath, dir)
			}
			return nil
		},
		func(file *os.File) error {
			fileSecured = true
			if filepath.Dir(file.Name()) != dir {
				t.Fatalf("secured file directory = %q, want %q", filepath.Dir(file.Name()), dir)
			}
			return nil
		},
	)
	if errWrite != nil {
		t.Fatalf("writeConfigAtomicWithSecurity() error = %v", errWrite)
	}
	if !directorySecured || !fileSecured || !replaced {
		t.Fatalf("directorySecured=%t fileSecured=%t replaced=%t, want all true", directorySecured, fileSecured, replaced)
	}
}

func TestWriteConfigAtomicWindowsSecurityFailureDoesNotReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	securityErr := errors.New("injected Windows DACL failure")
	replaced := false

	errWrite := writeConfigAtomicWithSecurity(
		path,
		[]byte("debug: true\n"),
		func(string, string) error {
			replaced = true
			return nil
		},
		func(string) error { return nil },
		func(*os.File) error { return securityErr },
	)
	if !errors.Is(errWrite, securityErr) {
		t.Fatalf("writeConfigAtomicWithSecurity() error = %v, want injected DACL failure", errWrite)
	}
	if replaced {
		t.Fatal("replace called after Windows DACL failure")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read original config: %v", errRead)
	}
	if string(data) != string(original) {
		t.Fatalf("original config = %q, want %q", data, original)
	}
	residual, errGlob := filepath.Glob(filepath.Join(dir, ".config.yaml-*.tmp"))
	if errGlob != nil {
		t.Fatalf("glob config temp files: %v", errGlob)
	}
	if len(residual) != 0 {
		t.Fatalf("residual config temp files = %#v, want none", residual)
	}
}

func TestWriteConfigAtomicRejectsWindowsReparseDirectoryAndPreservesOriginal(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if errMkdir := os.Mkdir(realDir, 0o700); errMkdir != nil {
		t.Fatalf("create real config directory: %v", errMkdir)
	}
	path := filepath.Join(realDir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	linkedDir := filepath.Join(root, "linked")
	if errSymlink := os.Symlink(realDir, linkedDir); errSymlink != nil {
		t.Skipf("Windows symlink creation unavailable: %v", errSymlink)
	}

	errWrite := WriteConfig(filepath.Join(linkedDir, "config.yaml"), []byte("debug: true\n"))
	if errWrite == nil {
		t.Fatal("WriteConfig() succeeded through a reparse-point config directory")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read original config: %v", errRead)
	}
	if string(data) != string(original) {
		t.Fatalf("original config = %q, want %q", data, original)
	}
	residual, errGlob := filepath.Glob(filepath.Join(realDir, ".config.yaml-*.tmp"))
	if errGlob != nil {
		t.Fatalf("glob config temp files: %v", errGlob)
	}
	if len(residual) != 0 {
		t.Fatalf("residual config temp files = %#v, want none", residual)
	}
}

func TestWriteConfigAtomicExistingWindowsVerifierFailureSkipsHardeningAndReplace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "managed")
	if errMkdir := os.Mkdir(dir, 0o700); errMkdir != nil {
		t.Fatalf("create existing config directory: %v", errMkdir)
	}
	path := filepath.Join(dir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	verifyErr := errors.New("injected unsafe existing Windows directory")
	hardened := false
	replaced := false

	errWrite := writeConfigAtomicWithSecurity(
		path,
		[]byte("debug: true\n"),
		func(string, string) error {
			replaced = true
			return nil
		},
		func(securedPath string) error {
			return secureConfigDirectoryWithOperations(
				securedPath,
				func(windows.Handle, string) error { return verifyErr },
				func(windows.Handle, string) error {
					hardened = true
					return nil
				},
			)
		},
		func(*os.File) error {
			t.Fatal("file security called after existing directory verification failure")
			return nil
		},
	)
	if !errors.Is(errWrite, verifyErr) {
		t.Fatalf("writeConfigAtomicWithSecurity() error = %v, want injected verifier failure", errWrite)
	}
	if hardened {
		t.Fatal("existing config directory hardener was called after verifier failure")
	}
	if replaced {
		t.Fatal("replace called after existing directory verifier failure")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read original config: %v", errRead)
	}
	if string(data) != string(original) {
		t.Fatalf("original config = %q, want %q", data, original)
	}
}

func TestSecureConfigDirectoryHardensNewWindowsFinalDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "parent", "managed")
	verified := false
	hardened := false

	errSecure := secureConfigDirectoryWithOperations(
		dir,
		func(windows.Handle, string) error {
			verified = true
			return nil
		},
		func(_ windows.Handle, securedPath string) error {
			hardened = true
			if securedPath != dir {
				t.Fatalf("hardened directory = %q, want %q", securedPath, dir)
			}
			return nil
		},
	)
	if errSecure != nil {
		t.Fatalf("secureConfigDirectoryWithOperations() error = %v", errSecure)
	}
	if verified {
		t.Fatal("existing directory verifier called for newly created final directory")
	}
	if !hardened {
		t.Fatal("newly created final directory was not hardened")
	}
}

func TestValidateWindowsConfigDirectoryACERejectsDangerousAllowsForOtherSIDs(t *testing.T) {
	currentUser := mustWindowsTestSID(t, "S-1-5-21-1000-1000-1000-1001")
	otherUser := mustWindowsTestSID(t, "S-1-5-21-2000-2000-2000-2002")

	tests := []struct {
		name string
		ace  []byte
	}{
		{
			name: "standard allow",
			ace:  windowsTestBasicACE(windows.ACCESS_ALLOWED_ACE_TYPE, 0, windows.FILE_WRITE_DATA, otherUser, nil),
		},
		{
			name: "inherited allow",
			ace:  windowsTestBasicACE(windows.ACCESS_ALLOWED_ACE_TYPE, windows.INHERITED_ACE, windows.FILE_APPEND_DATA, otherUser, nil),
		},
		{
			name: "compound allow",
			ace:  windowsTestCompoundACE(windows.GENERIC_WRITE, otherUser),
		},
		{
			name: "object allow",
			ace:  windowsTestObjectACE(windowsAccessAllowedObjectACEType, windows.FILE_WRITE_DATA, windows.ACE_OBJECT_TYPE_PRESENT, otherUser, nil),
		},
		{
			name: "callback allow",
			ace:  windowsTestBasicACE(windowsAccessAllowedCallbackACEType, 0, windows.FILE_APPEND_DATA, otherUser, []byte{1, 2, 3, 4}),
		},
		{
			name: "callback object allow",
			ace:  windowsTestObjectACE(windowsAccessAllowedCallbackObjectACEType, windows.WRITE_DAC, windows.ACE_INHERITED_OBJECT_TYPE_PRESENT, otherUser, []byte{5, 6, 7, 8}),
		},
		{
			name: "unknown dangerous ACE",
			ace:  windowsTestRawACE(0x7f, 0, windows.DELETE, nil),
		},
		{
			name: "unparseable object ACE",
			ace:  windowsTestRawACE(windowsAccessAllowedObjectACEType, 0, windows.WRITE_OWNER, []byte{4, 0, 0, 0}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if errValidate := validateWindowsTestACESequence(currentUser, test.ace); errValidate == nil {
				t.Fatal("dangerous allow ACE was accepted")
			}
		})
	}
}

func TestValidateWindowsConfigDirectoryACEAllowsReadOnlyAndTrustedSIDs(t *testing.T) {
	currentUser := mustWindowsTestSID(t, "S-1-5-21-1000-1000-1000-1001")
	otherUser := mustWindowsTestSID(t, "S-1-5-21-2000-2000-2000-2002")
	system := mustWindowsTestSID(t, "S-1-5-18")
	administrators := mustWindowsTestSID(t, "S-1-5-32-544")

	tests := []struct {
		name string
		ace  []byte
	}{
		{
			name: "other SID read only",
			ace:  windowsTestBasicACE(windows.ACCESS_ALLOWED_ACE_TYPE, 0, windows.FILE_GENERIC_READ, otherUser, nil),
		},
		{
			name: "current user",
			ace:  windowsTestBasicACE(windows.ACCESS_ALLOWED_ACE_TYPE, 0, windows.GENERIC_ALL, currentUser, nil),
		},
		{
			name: "SYSTEM",
			ace:  windowsTestObjectACE(windowsAccessAllowedObjectACEType, windows.WRITE_DAC, 0, system, nil),
		},
		{
			name: "Administrators",
			ace:  windowsTestBasicACE(windowsAccessAllowedCallbackACEType, 0, windows.WRITE_OWNER, administrators, []byte{1, 2, 3, 4}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if errValidate := validateWindowsTestACESequence(currentUser, test.ace); errValidate != nil {
				t.Fatalf("trusted or read-only ACE rejected: %v", errValidate)
			}
		})
	}
}

func TestValidateWindowsConfigDirectoryACERespectsDenyOrder(t *testing.T) {
	currentUser := mustWindowsTestSID(t, "S-1-5-21-1000-1000-1000-1001")
	otherUser := mustWindowsTestSID(t, "S-1-5-21-2000-2000-2000-2002")
	denyWrite := windowsTestBasicACE(windows.ACCESS_DENIED_ACE_TYPE, 0, windows.FILE_WRITE_DATA, otherUser, nil)
	allowWrite := windowsTestBasicACE(windows.ACCESS_ALLOWED_ACE_TYPE, 0, windows.FILE_WRITE_DATA, otherUser, nil)

	if errValidate := validateWindowsTestACESequence(currentUser, denyWrite, allowWrite); errValidate != nil {
		t.Fatalf("deny before matching allow rejected: %v", errValidate)
	}
	if errValidate := validateWindowsTestACESequence(currentUser, allowWrite, denyWrite); errValidate == nil {
		t.Fatal("allow before deny was accepted")
	}

	conditionalDeny := windowsTestBasicACE(windowsAccessDeniedCallbackACEType, 0, windows.FILE_WRITE_DATA, otherUser, []byte{1, 2, 3, 4})
	if errValidate := validateWindowsTestACESequence(currentUser, conditionalDeny, allowWrite); errValidate == nil {
		t.Fatal("conditional deny incorrectly suppressed a later dangerous allow")
	}

	malformedDeny := windowsTestBasicACE(windows.ACCESS_DENIED_ACE_TYPE, 0, windows.FILE_WRITE_DATA, otherUser, []byte{1, 2, 3, 4})
	if errValidate := validateWindowsTestACESequence(currentUser, malformedDeny, allowWrite); errValidate == nil {
		t.Fatal("malformed deny incorrectly suppressed a later dangerous allow")
	}

	partialDeny := windowsTestBasicACE(windows.ACCESS_DENIED_ACE_TYPE, 0, windows.FILE_APPEND_DATA, otherUser, nil)
	if errValidate := validateWindowsTestACESequence(currentUser, partialDeny, allowWrite); errValidate == nil {
		t.Fatal("partial deny incorrectly suppressed a different dangerous right")
	}
}

func validateWindowsTestACESequence(currentUser *windows.SID, aces ...[]byte) error {
	denied := make(map[string]windows.ACCESS_MASK)
	for aceIndex, ace := range aces {
		if errValidate := validateWindowsConfigDirectoryACE(ace, currentUser, denied, `C:\config`, uint16(aceIndex)); errValidate != nil {
			return errValidate
		}
	}
	return nil
}

func mustWindowsTestSID(t *testing.T, value string) *windows.SID {
	t.Helper()
	sid, errSID := windows.StringToSid(value)
	if errSID != nil {
		t.Fatalf("parse SID %q: %v", value, errSID)
	}
	return sid
}

func windowsTestBasicACE(aceType, aceFlags byte, mask windows.ACCESS_MASK, sid *windows.SID, trailing []byte) []byte {
	body := append(windowsTestSIDBytes(sid), trailing...)
	return windowsTestRawACE(aceType, aceFlags, mask, body)
}

func windowsTestCompoundACE(mask windows.ACCESS_MASK, sid *windows.SID) []byte {
	body := make([]byte, 4)
	body = append(body, windowsTestSIDBytes(sid)...)
	return windowsTestRawACE(windowsAccessAllowedCompoundACEType, 0, mask, body)
}

func windowsTestObjectACE(aceType byte, mask windows.ACCESS_MASK, objectFlags uint32, sid *windows.SID, trailing []byte) []byte {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, objectFlags)
	if objectFlags&windows.ACE_OBJECT_TYPE_PRESENT != 0 {
		body = append(body, make([]byte, 16)...)
	}
	if objectFlags&windows.ACE_INHERITED_OBJECT_TYPE_PRESENT != 0 {
		body = append(body, make([]byte, 16)...)
	}
	body = append(body, windowsTestSIDBytes(sid)...)
	body = append(body, trailing...)
	return windowsTestRawACE(aceType, 0, mask, body)
}

func windowsTestRawACE(aceType, aceFlags byte, mask windows.ACCESS_MASK, body []byte) []byte {
	ace := make([]byte, 8, 8+len(body))
	ace[0] = aceType
	ace[1] = aceFlags
	binary.LittleEndian.PutUint16(ace[2:4], uint16(8+len(body)))
	binary.LittleEndian.PutUint32(ace[4:8], uint32(mask))
	return append(ace, body...)
}

func windowsTestSIDBytes(sid *windows.SID) []byte {
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(sid)), sid.Len())...)
}
