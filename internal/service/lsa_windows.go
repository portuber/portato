//go:build windows

package service

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows service-logon plumbing for Phase 47. A service that runs under a
// real user account must:
//   - have that account's password (validated by SCM at Start time — but SCM
//     reports a bad password as a generic "logon failure" after the service is
//     already half-created), and
//   - hold the SeServiceLogonRight, which CreateService grants unreliably (on
//     many accounts/policies the right is absent and the service fails to start
//     with the same generic "logon failure").
//
// lsaValidateServiceCreds checks the password up front (LogonUser) so a wrong
// password / Windows Hello PIN / Microsoft-account mismatch is caught with a
// clear message before anything is written. lsaGrantServiceLogonRight grants
// the right explicitly via the LSA policy (LsaAddAccountRights), so install
// works out of the box for any valid account.

const (
	policyLookupNames      uint32 = 0x00000800
	policyCreateSecret     uint32 = 0x00000020
	logon32LogonNetwork    uint32 = 2
	logon32ProviderDefault uint32 = 0
)

type lsaUnicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type lsaObjectAttributes struct {
	Length                   uint32
	RootDirectory            windows.Handle
	ObjectName               *lsaUnicodeString
	Attributes               uint32
	SecurityDescriptor       *byte
	SecurityQualityOfService *byte
}

var (
	advapi32                = windows.NewLazySystemDLL("advapi32.dll")
	procLsaOpenPolicy       = advapi32.NewProc("LsaOpenPolicy")
	procLsaClose            = advapi32.NewProc("LsaClose")
	procLsaAddAccountRights = advapi32.NewProc("LsaAddAccountRights")
	procLsaNtStatusToWinErr = advapi32.NewProc("LsaNtStatusToWinError")
	procLogonUser           = advapi32.NewProc("LogonUserW")
)

// lsaGrantServiceLogonRight / lsaValidateServiceCreds are seams over the advapi32
// implementations so the installer flow is unit-testable without a real LSA /
// domain controller (the tests inject no-op fakes). Mirrors the execFunc /
// scmAPI seam pattern.
var (
	lsaGrantServiceLogonRight = grantServiceLogonRightImpl
	lsaValidateServiceCreds   = validateServiceCredsImpl
)

// grantServiceLogonRightImpl grants SeServiceLogonRight to account. Idempotent:
// a no-op if the right is already present. Returns nil for an empty account
// (LocalSystem already has the right).
func grantServiceLogonRightImpl(account string) error {
	if account == "" {
		return nil
	}
	sid, err := lookupAccountSid(account)
	if err != nil {
		return err
	}
	var handle uintptr
	oa := lsaObjectAttributes{Length: uint32(unsafe.Sizeof(lsaObjectAttributes{}))}
	if status, _, _ := procLsaOpenPolicy.Call(
		0,
		uintptr(unsafe.Pointer(&oa)),
		uintptr(policyLookupNames|policyCreateSecret),
		uintptr(unsafe.Pointer(&handle)),
	); status != 0 {
		return fmt.Errorf("open LSA policy: %w", lsaError(status))
	}
	defer procLsaClose.Call(handle)

	right := ntUnicodeString("SeServiceLogonRight")
	if status, _, _ := procLsaAddAccountRights.Call(
		handle,
		uintptr(unsafe.Pointer(sid)),
		uintptr(unsafe.Pointer(&right)),
		1,
	); status != 0 {
		return fmt.Errorf("grant SeServiceLogonRight to %s: %w", account, lsaError(status))
	}
	return nil
}

// validateServiceCredsImpl checks account/password via a network logon. It
// returns nil unless Windows reports ERROR_LOGON_FAILURE (the definitive
// "wrong password"): a Windows Hello PIN, a Microsoft-account cloud password,
// or a typo all surface here, so the install can fail with a clear message
// before a half-configured service is left behind. Other logon errors (e.g.
// blank-password or account-restriction policies) are treated as undetermined
// and do not block — the real service logon at Start is the final arbiter.
func validateServiceCredsImpl(account, password string) error {
	if account == "" {
		return nil
	}
	domain, user := splitAccountDomain(account)
	u, err := syscall.UTF16PtrFromString(user)
	if err != nil {
		return fmt.Errorf("encode user name: %w", err)
	}
	var d *uint16
	if domain != "" {
		if d, err = syscall.UTF16PtrFromString(domain); err != nil {
			return fmt.Errorf("encode domain: %w", err)
		}
	}
	p, err := syscall.UTF16PtrFromString(password)
	if err != nil {
		return fmt.Errorf("encode password: %w", err)
	}
	var token windows.Token
	r1, _, callErr := procLogonUser.Call(
		uintptr(unsafe.Pointer(u)),
		uintptr(unsafe.Pointer(d)),
		uintptr(unsafe.Pointer(p)),
		uintptr(logon32LogonNetwork),
		uintptr(logon32ProviderDefault),
		uintptr(unsafe.Pointer(&token)),
	)
	if r1 == 0 {
		if errors.Is(callErr, windows.ERROR_LOGON_FAILURE) {
			return fmt.Errorf("wrong password for %s (a Windows Hello PIN or Microsoft-account password is not accepted; enter the account's local password)", account)
		}
		return nil // undetermined (e.g., logon-type restricted) -> do not block
	}
	token.Close()
	return nil
}

// lookupAccountSid resolves account ("DOMAIN\\user" / ".\\user" / "user") to its
// SID (a SECURITY_MAX_SID_SIZE buffer cast to *windows.SID).
func lookupAccountSid(account string) (*windows.SID, error) {
	name := account
	if strings.HasPrefix(account, `.\`) {
		name = account[2:] // "." means the local machine; look it up locally
	}
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var (
		sidBuf   [256]byte
		sidLen   = uint32(len(sidBuf))
		domainBf [256]uint16
		domainLn = uint32(len(domainBf))
		use      uint32
	)
	sid := (*windows.SID)(unsafe.Pointer(&sidBuf[0]))
	if err := windows.LookupAccountName(nil, namePtr, sid, &sidLen, &domainBf[0], &domainLn, &use); err != nil {
		return nil, fmt.Errorf("look up account %s: %w", account, err)
	}
	return sid, nil
}

// splitAccountDomain splits "DOMAIN\\user" / ".\\user" / "user" into (domain,
// user). domain is "" for local ("." or bare).
func splitAccountDomain(account string) (domain, user string) {
	if i := strings.IndexByte(account, '\\'); i >= 0 {
		d := account[:i]
		if d == "." {
			d = ""
		}
		return d, account[i+1:]
	}
	return "", account
}

// ntUnicodeString builds an LSA_UNICODE_STRING for s (the buffer is the slice
// the struct points at, kept alive for the duration of the LSA call).
func ntUnicodeString(s string) lsaUnicodeString {
	w := syscall.StringToUTF16(s) // []uint16 including a terminating 0
	n := uint16((len(w) - 1) * 2) // bytes, excluding the terminator
	return lsaUnicodeString{Length: n, MaximumLength: n, Buffer: &w[0]}
}

func lsaError(status uintptr) error {
	r, _, _ := procLsaNtStatusToWinErr.Call(status)
	if r == 0 {
		return fmt.Errorf("NTSTATUS 0x%x", uint32(status))
	}
	return syscall.Errno(uint32(r))
}
