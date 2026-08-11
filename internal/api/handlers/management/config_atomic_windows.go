//go:build windows

package management

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const configDirectoryAccess = windows.FILE_GENERIC_READ |
	windows.FILE_GENERIC_WRITE |
	windows.FILE_GENERIC_EXECUTE |
	windows.DELETE |
	windows.ACCESS_MASK(0x40)

const configFileAccess = windows.FILE_GENERIC_READ |
	windows.FILE_GENERIC_WRITE |
	windows.DELETE

const dangerousConfigDirectoryAccess = windows.FILE_WRITE_DATA |
	windows.FILE_APPEND_DATA |
	windows.FILE_WRITE_EA |
	windows.FILE_WRITE_ATTRIBUTES |
	windows.ACCESS_MASK(0x40) |
	windows.DELETE |
	windows.WRITE_DAC |
	windows.WRITE_OWNER

const (
	windowsAccessAllowedCompoundACEType       = 4
	windowsAccessAllowedObjectACEType         = 5
	windowsAccessDeniedObjectACEType          = 6
	windowsAccessAllowedCallbackACEType       = 9
	windowsAccessDeniedCallbackACEType        = 10
	windowsAccessAllowedCallbackObjectACEType = 11
	windowsAccessDeniedCallbackObjectACEType  = 12
)

type windowsConfigACEKind uint8

const (
	windowsConfigACEIgnored windowsConfigACEKind = iota
	windowsConfigACEAllow
	windowsConfigACEDeny
	windowsConfigACEConditionalDeny
)

type windowsConfigDirectoryVerifier func(windows.Handle, string) error
type windowsConfigDirectoryMutator func(windows.Handle, string) error

func secureConfigDirectory(path string) error {
	return secureConfigDirectoryWithOperations(path, validateExistingWindowsConfigDirectory, hardenNewWindowsConfigDirectory)
}

func secureConfigDirectoryWithOperations(path string, verify windowsConfigDirectoryVerifier, harden windowsConfigDirectoryMutator) error {
	absolutePath, errAbs := filepath.Abs(path)
	if errAbs != nil {
		return fmt.Errorf("resolve config directory path: %w", errAbs)
	}
	finalCreated, errPrepare := prepareConfigDirectoryNoReparse(absolutePath)
	if errPrepare != nil {
		return errPrepare
	}

	access := uint32(windows.READ_CONTROL)
	if finalCreated {
		access |= windows.WRITE_DAC | windows.WRITE_OWNER
	}
	handle, errOpen := openWindowsConfigDirectory(absolutePath, access)
	if errOpen != nil {
		return fmt.Errorf("open config directory for security validation: %w", errOpen)
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()
	if errValidate := validateWindowsConfigDirectoryHandle(handle, absolutePath); errValidate != nil {
		return errValidate
	}
	if finalCreated {
		if harden == nil {
			return fmt.Errorf("config directory hardening function is unavailable")
		}
		return harden(handle, absolutePath)
	}
	if verify == nil {
		return fmt.Errorf("config directory verifier is unavailable")
	}
	return verify(handle, absolutePath)
}

func prepareConfigDirectoryNoReparse(path string) (bool, error) {
	volume := filepath.VolumeName(path)
	currentPath := volume + string(filepath.Separator)
	remainder := strings.TrimPrefix(path, currentPath)
	components := windowsConfigDirectoryComponents(remainder)
	finalCreated := false
	for componentIndex, component := range components {
		currentPath = filepath.Join(currentPath, component)
		created := false
		if errMkdir := os.Mkdir(currentPath, 0o700); errMkdir != nil {
			if !errors.Is(errMkdir, os.ErrExist) {
				return false, fmt.Errorf("create config directory component %q: %w", component, errMkdir)
			}
		} else {
			created = true
		}
		if componentIndex == len(components)-1 {
			finalCreated = created
		}
		handle, errOpen := openWindowsConfigDirectory(currentPath, 0)
		if errOpen != nil {
			return false, fmt.Errorf("open config directory component %q without following reparse points: %w", component, errOpen)
		}
		errValidate := validateWindowsConfigDirectoryHandle(handle, currentPath)
		errClose := windows.CloseHandle(handle)
		if errValidate != nil {
			return false, errValidate
		}
		if errClose != nil {
			return false, fmt.Errorf("close config directory component %q: %w", component, errClose)
		}
	}
	return finalCreated, nil
}

func windowsConfigDirectoryComponents(path string) []string {
	components := strings.Split(path, string(filepath.Separator))
	filtered := components[:0]
	for _, component := range components {
		if component != "" && component != "." {
			filtered = append(filtered, component)
		}
	}
	return filtered
}

func openWindowsConfigDirectory(path string, access uint32) (windows.Handle, error) {
	pathUTF16, errPath := windows.UTF16PtrFromString(path)
	if errPath != nil {
		return windows.InvalidHandle, errPath
	}
	return windows.CreateFile(
		pathUTF16,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
}

func validateWindowsConfigDirectoryHandle(handle windows.Handle, path string) error {
	var info windows.ByHandleFileInformation
	if errInfo := windows.GetFileInformationByHandle(handle, &info); errInfo != nil {
		return fmt.Errorf("inspect config directory %q: %w", path, errInfo)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("config directory %q is a reparse point", path)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return fmt.Errorf("config directory %q is not a real directory", path)
	}
	return nil
}

func validateExistingWindowsConfigDirectory(handle windows.Handle, path string) error {
	return validateWindowsConfigDirectorySecurity(handle, path, false)
}

func validateWindowsConfigDirectorySecurity(handle windows.Handle, path string, requireProtected bool) error {
	descriptor, errSecurity := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if errSecurity != nil {
		return fmt.Errorf("read config directory security %q: %w", path, errSecurity)
	}
	owner, _, errOwner := descriptor.Owner()
	if errOwner != nil {
		return fmt.Errorf("read config directory owner %q: %w", path, errOwner)
	}
	if errAllowedOwner := validateWindowsConfigDirectoryOwner(owner); errAllowedOwner != nil {
		return fmt.Errorf("config directory %q: %w", path, errAllowedOwner)
	}
	if requireProtected {
		control, _, errControl := descriptor.Control()
		if errControl != nil {
			return fmt.Errorf("read config directory DACL control %q: %w", path, errControl)
		}
		if control&windows.SE_DACL_PROTECTED == 0 {
			return fmt.Errorf("newly created config directory %q DACL is not protected", path)
		}
	}
	dacl, _, errDACL := descriptor.DACL()
	if errDACL != nil {
		return fmt.Errorf("read config directory DACL %q: %w", path, errDACL)
	}
	if dacl == nil {
		return fmt.Errorf("config directory %q has a permissive null DACL", path)
	}
	return validateWindowsConfigDirectoryDACL(dacl, path)
}

func validateWindowsConfigDirectoryOwner(owner *windows.SID) error {
	if owner == nil {
		return fmt.Errorf("owner is missing")
	}
	processUser, errUser := windows.GetCurrentProcessToken().GetTokenUser()
	if errUser != nil {
		return errUser
	}
	if owner.Equals(processUser.User.Sid) || owner.IsWellKnown(windows.WinLocalSystemSid) || owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return nil
	}
	return fmt.Errorf("owner %s is not the current user, SYSTEM, or Administrators", owner.String())
}

func validateWindowsConfigDirectoryDACL(dacl *windows.ACL, path string) error {
	processUser, errUser := windows.GetCurrentProcessToken().GetTokenUser()
	if errUser != nil {
		return fmt.Errorf("read current user for config directory DACL %q: %w", path, errUser)
	}
	denied := make(map[string]windows.ACCESS_MASK)
	for aceIndex := uint16(0); aceIndex < dacl.AceCount; aceIndex++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if errACE := windows.GetAce(dacl, uint32(aceIndex), &ace); errACE != nil {
			return fmt.Errorf("read config directory DACL entry %d for %q: %w", aceIndex, path, errACE)
		}
		if ace.Header.AceSize < uint16(unsafe.Sizeof(windows.ACE_HEADER{})) {
			return fmt.Errorf("config directory %q has invalid DACL entry %d size %d", path, aceIndex, ace.Header.AceSize)
		}
		aceBytes := unsafe.Slice((*byte)(unsafe.Pointer(ace)), int(ace.Header.AceSize))
		if errValidate := validateWindowsConfigDirectoryACE(aceBytes, processUser.User.Sid, denied, path, aceIndex); errValidate != nil {
			return errValidate
		}
	}
	runtime.KeepAlive(processUser)
	return nil
}

func validateWindowsConfigDirectoryACE(aceBytes []byte, currentUser *windows.SID, denied map[string]windows.ACCESS_MASK, path string, aceIndex uint16) error {
	if len(aceBytes) < 8 {
		return fmt.Errorf("config directory %q has truncated DACL entry %d", path, aceIndex)
	}
	aceType := aceBytes[0]
	aceFlags := aceBytes[1]
	aceSize := int(binary.LittleEndian.Uint16(aceBytes[2:4]))
	if aceSize != len(aceBytes) {
		return fmt.Errorf("config directory %q has inconsistent DACL entry %d size %d", path, aceIndex, aceSize)
	}
	mask := windows.ACCESS_MASK(binary.LittleEndian.Uint32(aceBytes[4:8]))
	rights := normalizedDangerousConfigDirectoryAccess(mask)
	if aceFlags&windows.INHERIT_ONLY_ACE != 0 {
		return nil
	}

	kind, sidOffset, trailingAllowed, errLayout := windowsConfigACELayout(aceType, aceBytes)
	if errLayout != nil {
		if rights != 0 {
			return fmt.Errorf("config directory %q has unparseable DACL entry %d that may grant dangerous write access: %w", path, aceIndex, errLayout)
		}
		return nil
	}
	if kind == windowsConfigACEIgnored {
		if windowsConfigACETypeKnown(aceType) {
			return nil
		}
		if rights != 0 {
			return fmt.Errorf("config directory %q has unknown DACL entry type %d at index %d that may grant dangerous write access", path, aceType, aceIndex)
		}
		return nil
	}

	sid, sidEnd, errSID := windowsConfigACESID(aceBytes, sidOffset)
	if errSID != nil {
		if rights != 0 {
			return fmt.Errorf("config directory %q has unparseable DACL entry %d that may grant dangerous write access: %w", path, aceIndex, errSID)
		}
		return nil
	}
	if !trailingAllowed && sidEnd != len(aceBytes) {
		if rights != 0 {
			return fmt.Errorf("config directory %q has DACL entry %d with unexpected trailing data that may affect dangerous write access", path, aceIndex)
		}
		return nil
	}
	sidString := sid.String()
	if sidString == "" {
		if rights != 0 {
			return fmt.Errorf("config directory %q has DACL entry %d with an unreadable SID that may grant dangerous write access", path, aceIndex)
		}
		return nil
	}

	switch kind {
	case windowsConfigACEDeny:
		denied[sidString] |= rights
		return nil
	case windowsConfigACEConditionalDeny:
		return nil
	case windowsConfigACEAllow:
		if rights == 0 || windowsConfigDirectorySIDAllowed(sid, currentUser) {
			return nil
		}
		effectiveRights := rights &^ denied[sidString]
		if effectiveRights != 0 {
			return fmt.Errorf("config directory %q grants dangerous write access to %s in DACL entry %d", path, sidString, aceIndex)
		}
		return nil
	default:
		return fmt.Errorf("config directory %q has unsupported DACL entry %d", path, aceIndex)
	}
}

func windowsConfigACELayout(aceType byte, aceBytes []byte) (windowsConfigACEKind, int, bool, error) {
	switch aceType {
	case windows.ACCESS_ALLOWED_ACE_TYPE:
		return windowsConfigACEAllow, 8, false, nil
	case windows.ACCESS_DENIED_ACE_TYPE:
		return windowsConfigACEDeny, 8, false, nil
	case windowsAccessAllowedCompoundACEType:
		if len(aceBytes) < 12 {
			return windowsConfigACEIgnored, 0, false, fmt.Errorf("truncated compound allow ACE")
		}
		return windowsConfigACEAllow, 12, false, nil
	case windowsAccessAllowedObjectACEType:
		sidOffset, errOffset := windowsConfigObjectACESIDOffset(aceBytes)
		return windowsConfigACEAllow, sidOffset, false, errOffset
	case windowsAccessAllowedCallbackObjectACEType:
		sidOffset, errOffset := windowsConfigObjectACESIDOffset(aceBytes)
		return windowsConfigACEAllow, sidOffset, true, errOffset
	case windowsAccessDeniedObjectACEType:
		sidOffset, errOffset := windowsConfigObjectACESIDOffset(aceBytes)
		return windowsConfigACEConditionalDeny, sidOffset, false, errOffset
	case windowsAccessDeniedCallbackObjectACEType:
		sidOffset, errOffset := windowsConfigObjectACESIDOffset(aceBytes)
		return windowsConfigACEConditionalDeny, sidOffset, true, errOffset
	case windowsAccessAllowedCallbackACEType:
		return windowsConfigACEAllow, 8, true, nil
	case windowsAccessDeniedCallbackACEType:
		return windowsConfigACEConditionalDeny, 8, true, nil
	default:
		return windowsConfigACEIgnored, 0, false, nil
	}
}

func windowsConfigObjectACESIDOffset(aceBytes []byte) (int, error) {
	if len(aceBytes) < 12 {
		return 0, fmt.Errorf("truncated object ACE")
	}
	objectFlags := binary.LittleEndian.Uint32(aceBytes[8:12])
	if objectFlags&^(uint32(windows.ACE_OBJECT_TYPE_PRESENT)|uint32(windows.ACE_INHERITED_OBJECT_TYPE_PRESENT)) != 0 {
		return 0, fmt.Errorf("object ACE has unsupported flags %#x", objectFlags)
	}
	sidOffset := 12
	if objectFlags&windows.ACE_OBJECT_TYPE_PRESENT != 0 {
		sidOffset += 16
	}
	if objectFlags&windows.ACE_INHERITED_OBJECT_TYPE_PRESENT != 0 {
		sidOffset += 16
	}
	if sidOffset > len(aceBytes) {
		return 0, fmt.Errorf("truncated object ACE GUID data")
	}
	return sidOffset, nil
}

func windowsConfigACESID(aceBytes []byte, sidOffset int) (*windows.SID, int, error) {
	if sidOffset < 0 || sidOffset+8 > len(aceBytes) {
		return nil, 0, fmt.Errorf("missing SID")
	}
	sidLength := 8 + 4*int(aceBytes[sidOffset+1])
	if sidOffset+sidLength > len(aceBytes) {
		return nil, 0, fmt.Errorf("truncated SID")
	}
	sid := (*windows.SID)(unsafe.Pointer(&aceBytes[sidOffset]))
	if !sid.IsValid() || sid.Len() != sidLength {
		return nil, 0, fmt.Errorf("invalid SID")
	}
	return sid, sidOffset + sidLength, nil
}

func windowsConfigACETypeKnown(aceType byte) bool {
	return aceType <= 21
}

func normalizedDangerousConfigDirectoryAccess(mask windows.ACCESS_MASK) windows.ACCESS_MASK {
	rights := mask & dangerousConfigDirectoryAccess
	if mask&windows.GENERIC_WRITE != 0 {
		rights |= windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES
	}
	if mask&windows.GENERIC_ALL != 0 {
		rights |= dangerousConfigDirectoryAccess
	}
	return rights
}

func windowsConfigDirectorySIDAllowed(sid, currentUser *windows.SID) bool {
	return sid.Equals(currentUser) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid)
}

func hardenNewWindowsConfigDirectory(handle windows.Handle, path string) error {
	acl, errACL := configProtectedACL(configDirectoryAccess, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)
	if errACL != nil {
		return errACL
	}
	processUser, errUser := windows.GetCurrentProcessToken().GetTokenUser()
	if errUser != nil {
		return errUser
	}
	if errSecurity := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		processUser.User.Sid,
		nil,
		acl,
		nil,
	); errSecurity != nil {
		return fmt.Errorf("harden newly created config directory %q: %w", path, errSecurity)
	}
	runtime.KeepAlive(processUser)
	runtime.KeepAlive(acl)
	return validateWindowsConfigDirectorySecurity(handle, path, true)
}

func secureConfigFile(file *os.File) error {
	acl, errACL := configProtectedACL(configFileAccess, windows.NO_INHERITANCE)
	if errACL != nil {
		return errACL
	}
	if errSecurity := windows.SetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); errSecurity != nil {
		return errSecurity
	}
	runtime.KeepAlive(acl)
	return nil
}

func configProtectedACL(access windows.ACCESS_MASK, inheritance uint32) (*windows.ACL, error) {
	processUser, errUser := windows.GetCurrentProcessToken().GetTokenUser()
	if errUser != nil {
		return nil, errUser
	}
	systemSID, errSystem := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if errSystem != nil {
		return nil, errSystem
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(processUser.User.Sid)
	pinner.Pin(systemSID)
	entries := []windows.EXPLICIT_ACCESS{
		configAccessEntry(processUser.User.Sid, windows.TRUSTEE_IS_USER, access, inheritance),
		configAccessEntry(systemSID, windows.TRUSTEE_IS_USER, access, inheritance),
	}
	return windows.ACLFromEntries(entries, nil)
}

func configAccessEntry(sid *windows.SID, trusteeType windows.TRUSTEE_TYPE, access windows.ACCESS_MASK, inheritance uint32) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: access,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  trusteeType,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

func replaceConfigFile(source, destination string) error {
	sourceUTF16, errSource := windows.UTF16PtrFromString(source)
	if errSource != nil {
		return errSource
	}
	destinationUTF16, errDestination := windows.UTF16PtrFromString(destination)
	if errDestination != nil {
		return errDestination
	}
	return windows.MoveFileEx(sourceUTF16, destinationUTF16, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func syncConfigDirectory(string) error {
	return nil
}
