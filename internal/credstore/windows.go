//go:build windows

package credstore

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errElementNotFound      = syscall.Errno(1168) // ERROR_NOT_FOUND
)

var (
	modAdvapi32    = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW = modAdvapi32.NewProc("CredWriteW")
	procCredReadW  = modAdvapi32.NewProc("CredReadW")
	procCredDelete = modAdvapi32.NewProc("CredDeleteW")
	procCredFree   = modAdvapi32.NewProc("CredFree")
)

type winCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type WindowsStore struct{}

func NewDefaultStore() Store {
	return &WindowsStore{}
}

func (w *WindowsStore) BackendName() string {
	return "windows-credential-manager"
}

func (w *WindowsStore) Set(target, username, secret string) error {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("encode target name: %w", err)
	}
	userPtr, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return fmt.Errorf("encode username: %w", err)
	}
	secretBytes := []byte(secret)
	var blobPtr *byte
	if len(secretBytes) > 0 {
		blobPtr = &secretBytes[0]
	}

	cred := winCredential{
		Type:               credTypeGeneric,
		TargetName:         targetPtr,
		UserName:           userPtr,
		CredentialBlobSize: uint32(len(secretBytes)),
		CredentialBlob:     blobPtr,
		Persist:            credPersistLocalMachine,
	}

	r1, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		return fmt.Errorf("CredWriteW failed: %w", callErr)
	}
	return nil
}

func (w *WindowsStore) Get(target string) (Credential, error) {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return Credential{}, fmt.Errorf("encode target name: %w", err)
	}

	var credPtr *winCredential
	r1, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&credPtr)),
	)
	if r1 == 0 {
		if errors.Is(callErr, errElementNotFound) {
			return Credential{}, ErrNotFound
		}
		return Credential{}, fmt.Errorf("CredReadW failed: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))

	var secret string
	if credPtr.CredentialBlobSize > 0 && credPtr.CredentialBlob != nil {
		blob := unsafe.Slice(credPtr.CredentialBlob, credPtr.CredentialBlobSize)
		secret = string(blob)
	}
	var user string
	if credPtr.UserName != nil {
		user = windows.UTF16PtrToString(credPtr.UserName)
	}
	return Credential{
		Target:   target,
		Username: user,
		Secret:   secret,
	}, nil
}

func (w *WindowsStore) Delete(target string) error {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("encode target name: %w", err)
	}
	r1, _, callErr := procCredDelete.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		credTypeGeneric,
		0,
	)
	if r1 == 0 && !errors.Is(callErr, errElementNotFound) {
		return fmt.Errorf("CredDeleteW failed: %w", callErr)
	}
	return nil
}
