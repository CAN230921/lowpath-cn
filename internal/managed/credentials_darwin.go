//go:build darwin && cgo

package managed

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>

static CFMutableDictionaryRef loginQuery(const char *gateway) {
	CFStringRef account = CFStringCreateWithCString(NULL, gateway, kCFStringEncodingUTF8);
	if (account == NULL) return NULL;
	CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (query != NULL) {
		CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
		CFDictionarySetValue(query, kSecAttrService, CFSTR("Lowpath CN saved login"));
		CFDictionarySetValue(query, kSecAttrAccount, account);
	}
	CFRelease(account);
	return query;
}

static OSStatus loadLogin(const char *gateway, CFDataRef *data) {
	CFMutableDictionaryRef query = loginQuery(gateway);
	if (query == NULL) return errSecAllocate;
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	OSStatus status = SecItemCopyMatching(query, (CFTypeRef *)data);
	CFRelease(query);
	return status;
}

static OSStatus saveLogin(const char *gateway, const UInt8 *value, CFIndex length) {
	CFMutableDictionaryRef query = loginQuery(gateway);
	CFDataRef data = CFDataCreate(NULL, value, length);
	if (query == NULL || data == NULL) {
		if (query != NULL) CFRelease(query);
		if (data != NULL) CFRelease(data);
		return errSecAllocate;
	}
	const void *keys[] = {kSecValueData};
	const void *values[] = {data};
	CFDictionaryRef update = CFDictionaryCreate(NULL, keys, values, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	OSStatus status = errSecAllocate;
	if (update != NULL) {
		status = SecItemUpdate(query, update);
		if (status == errSecItemNotFound) {
			CFDictionarySetValue(query, kSecValueData, data);
			status = SecItemAdd(query, NULL);
		}
		CFRelease(update);
	}
	CFRelease(data);
	CFRelease(query);
	return status;
}

static OSStatus deleteLogin(const char *gateway) {
	CFMutableDictionaryRef query = loginQuery(gateway);
	if (query == NULL) return errSecAllocate;
	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	return status;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

type keychainStore string

func newCredentialStore(gateway string) CredentialStore { return keychainStore(gateway) }

func (s keychainStore) Load() (string, error) {
	account := C.CString(string(s))
	defer C.free(unsafe.Pointer(account))
	var data C.CFDataRef
	status := C.loadLogin(account, &data)
	if status == C.errSecItemNotFound {
		return "", nil
	}
	if status != C.errSecSuccess {
		return "", keychainError(status)
	}
	defer C.CFRelease(C.CFTypeRef(data))
	length := C.CFDataGetLength(data)
	if length != credentialLength {
		return "", errors.New("saved login credential is invalid")
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(C.CFDataGetBytePtr(data))), C.int(length)), nil
}

func (s keychainStore) Save(value string) error {
	if len(value) != credentialLength {
		return errors.New("login credential is invalid")
	}
	account := C.CString(string(s))
	defer C.free(unsafe.Pointer(account))
	data := []byte(value)
	defer clear(data)
	status := C.saveLogin(account, (*C.UInt8)(&data[0]), C.CFIndex(len(data)))
	if status != C.errSecSuccess {
		return keychainError(status)
	}
	return nil
}

func (s keychainStore) Delete() error {
	account := C.CString(string(s))
	defer C.free(unsafe.Pointer(account))
	status := C.deleteLogin(account)
	if status != C.errSecSuccess && status != C.errSecItemNotFound {
		return keychainError(status)
	}
	return nil
}

func keychainError(status C.OSStatus) error {
	return fmt.Errorf("macOS Keychain error %d", int(status))
}
