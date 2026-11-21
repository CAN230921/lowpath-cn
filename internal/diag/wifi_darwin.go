//go:build cgo

package diag

/*
#cgo LDFLAGS: -framework Foundation -framework CoreWLAN
#include <stdlib.h>
char *lowpath_wifi(unsigned int index, int survey, int *status);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
	"unsafe"
)

type darwinWLAN struct{ unsupportedWLAN }

var darwinLocationDenied = fmt.Errorf("%w: macOS 未向 Lowpath 提供接入点标识。可以在系统设置 → 隐私与安全性 → 定位服务中检查应用授权。", ErrLocationDenied)

func newPlatformWLAN() WLAN { return darwinWLAN{} }

type darwinRadio struct {
	Interface string  `json:"interface"`
	Frequency int     `json:"frequency"`
	Width     int     `json:"width"`
	RSSI      int     `json:"rssi"`
	Rate      float64 `json:"rate"`
	PHY       uint32  `json:"phy"`
}

func darwinWiFi(index, survey int, value any) error {
	var status C.int
	out := C.lowpath_wifi(C.uint(index), C.int(survey), &status)
	switch status {
	case 1:
		return ErrNoWiFi
	case 2:
		return ErrWiFiDisconnected
	case 3:
		return darwinLocationDenied
	case 4:
		return errors.New("diag: macOS has no cached Wi-Fi survey")
	}
	if out == nil {
		return errors.New("diag: CoreWLAN did not return link data")
	}
	defer C.free(unsafe.Pointer(out))
	return json.Unmarshal([]byte(C.GoString(out)), value)
}

func (darwinWLAN) Link(index int) (WiFiLink, error) {
	var radio darwinRadio
	if err := darwinWiFi(index, 0, &radio); err != nil {
		return WiFiLink{}, err
	}
	link := WiFiLink{At: time.Now(), Interface: radio.Interface, Adapter: "Wi-Fi", Source: "CoreWLAN",
		RSSI: validRSSI(int32(radio.RSSI))}
	link.fillChannel(radio.Frequency)
	if radio.Width > 0 {
		link.WidthMHz = &radio.Width
	}
	if radio.Rate > 0 {
		link.TxMbps = &radio.Rate
	}
	// CoreWLAN and the existing Windows decoder use different PHY numbers.
	phys := [...]uint32{0, 4, 5, 6, 7, 8, 10, 11}
	if radio.PHY < uint32(len(phys)) {
		link.Standard, link.Generation = phyStandard(phys[radio.PHY], link.Band)
	}
	return link, nil
}

func (darwinWLAN) Survey(index int) (WiFiSurvey, error) {
	var scan struct {
		Frequency int    `json:"frequency"`
		BSSID     string `json:"bssid"`
		Entries   []struct {
			BSSID     string `json:"bssid"`
			Frequency int    `json:"frequency"`
			RSSI      int    `json:"rssi"`
			IEs       []byte `json:"ies"`
		} `json:"entries"`
	}
	if err := darwinWiFi(index, 1, &scan); err != nil {
		return WiFiSurvey{}, err
	}
	own, err := net.ParseMAC(scan.BSSID)
	if err != nil || len(own) != 6 {
		return WiFiSurvey{}, darwinLocationDenied
	}
	entries := make([]BSSEntry, 0, len(scan.Entries))
	for _, candidate := range scan.Entries {
		mac, err := net.ParseMAC(candidate.BSSID)
		if err == nil && len(mac) == 6 {
			entries = append(entries, BSSEntry{BSSID: [6]byte(mac), FreqMHz: candidate.Frequency,
				RSSI: candidate.RSSI, IEs: candidate.IEs})
		}
	}
	return NewWiFiSurvey(time.Now(), [6]byte(own), scan.Frequency, entries), nil
}
