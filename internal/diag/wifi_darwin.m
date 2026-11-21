#import <CoreWLAN/CoreWLAN.h>
#import <Foundation/Foundation.h>
#include <net/if.h>
#include <stdlib.h>
#include <string.h>

static int frequency(CWChannel *channel) {
    int number = (int)channel.channelNumber;
    if (number <= 0) return 0;
    switch (channel.channelBand) {
        case kCWChannelBand2GHz: return number == 14 ? 2484 : 2407 + 5 * number;
        case kCWChannelBand5GHz: return (number >= 182 ? 4000 : 5000) + 5 * number;
        case 3: return number == 2 ? 5935 : 5950 + 5 * number; // 6 GHz
        default: return 0;
    }
}

static int channelWidth(CWChannel *channel) {
    switch (channel.channelWidth) {
        case kCWChannelWidth20MHz: return 20;
        case kCWChannelWidth40MHz: return 40;
        case kCWChannelWidth80MHz: return 80;
        case kCWChannelWidth160MHz: return 160;
        default: return 0;
    }
}

// The returned JSON is private to the reader. No SSIDs are collected. BSSIDs
// are used only to distinguish the connected access point from its neighbors.
char *lowpath_wifi(unsigned int index, int survey, int *status) {
    @autoreleasepool {
        CWWiFiClient *client = [CWWiFiClient sharedWiFiClient];
        CWInterface *interface = nil;
        for (CWInterface *candidate in client.interfaces) {
            if (index != 0 && if_nametoindex(candidate.interfaceName.UTF8String) != index) continue;
            if (interface == nil || candidate.interfaceMode == kCWInterfaceModeStation) interface = candidate;
            if (index != 0 || candidate.interfaceMode == kCWInterfaceModeStation) break;
        }
        if (interface == nil) {
            *status = 1;
            return NULL;
        }
        if (!interface.powerOn || interface.interfaceMode != kCWInterfaceModeStation) {
            *status = 2;
            return NULL;
        }
        int freq = frequency(interface.wlanChannel);
        NSDictionary *value;
        if (survey) {
            NSString *own = interface.bssid;
            if (own.length == 0 || freq == 0) {
                *status = 3;
                return NULL;
            }
            NSSet<CWNetwork *> *cached = interface.cachedScanResults;
            if (cached.count == 0) {
                *status = 4;
                return NULL;
            }
            NSMutableArray *entries = [NSMutableArray arrayWithCapacity:cached.count];
            for (CWNetwork *network in cached) {
                int candidate = frequency(network.wlanChannel);
                if (network.bssid.length == 0 || candidate == 0) continue;
                NSString *elements = [network.informationElementData base64EncodedStringWithOptions:0];
                [entries addObject:@{@"bssid": network.bssid, @"frequency": @(candidate),
                    @"rssi": @(network.rssiValue), @"ies": elements ?: @""}];
            }
            value = @{@"frequency": @(freq), @"bssid": own, @"entries": entries};
        } else {
            value = @{@"interface": interface.interfaceName ?: @"", @"frequency": @(freq),
                @"width": @(channelWidth(interface.wlanChannel)), @"rssi": @(interface.rssiValue),
                @"rate": @(interface.transmitRate), @"phy": @(interface.activePHYMode)};
        }
        NSData *payload = [NSJSONSerialization dataWithJSONObject:value options:0 error:NULL];
        NSString *json = [[[NSString alloc] initWithData:payload encoding:NSUTF8StringEncoding] autorelease];
        *status = 0;
        return json == nil ? NULL : strdup(json.UTF8String);
    }
}
