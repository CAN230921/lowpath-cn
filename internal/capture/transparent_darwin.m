#import <Foundation/Foundation.h>
#import <NetworkExtension/NetworkExtension.h>
#import <SystemExtensions/SystemExtensions.h>
#import <CoreFoundation/CoreFoundation.h>

static NSString *providerIdentifier(void) {
    NSString *bundleID = NSBundle.mainBundle.bundleIdentifier;
    if (bundleID.length == 0) return nil;
    return [bundleID stringByAppendingString:@".routing"];
}

int lowpath_proxy_embedded(void) {
    @autoreleasepool {
        if (@available(macOS 15.0, *)) {
            NSString *identifier = providerIdentifier();
            if (identifier == nil) return 0;
            NSString *folder = [NSBundle.mainBundle.bundlePath stringByAppendingPathComponent:
                @"Contents/Library/SystemExtensions"];
            NSString *path = [folder stringByAppendingPathComponent:
                [identifier stringByAppendingString:@".systemextension"]];
            return [NSFileManager.defaultManager fileExistsAtPath:path];
        }
        return 0;
    }
}

@interface LowpathExtensionRequest : NSObject <OSSystemExtensionRequestDelegate>
@property(nonatomic, copy) NSString *bridge;
@end

static void controlError(NSError *error) {
    fprintf(stderr, "%s\n", error.localizedDescription.UTF8String ?: "Network extension error");
    exit(1);
}

@implementation LowpathExtensionRequest
- (OSSystemExtensionReplacementAction)request:(OSSystemExtensionRequest *)request
                         actionForReplacingExtension:(OSSystemExtensionProperties *)old
                                     withExtension:(OSSystemExtensionProperties *)replacement {
    return OSSystemExtensionReplacementActionReplace;
}

- (void)requestNeedsUserApproval:(OSSystemExtensionRequest *)request {
    fprintf(stderr, "请在系统设置中批准 Lowpath 网络扩展。\n");
}

- (void)request:(OSSystemExtensionRequest *)request didFailWithError:(NSError *)error {
    controlError(error);
}

- (void)request:(OSSystemExtensionRequest *)request didFinishWithResult:(OSSystemExtensionRequestResult)result {
    if (result != OSSystemExtensionRequestCompleted) exit(1);
    NSString *identifier = providerIdentifier();
    [NETransparentProxyManager loadAllFromPreferencesWithCompletionHandler:
        ^(NSArray<NETransparentProxyManager *> *managers, NSError *failure) {
        if (failure != nil) controlError(failure);
        NETransparentProxyManager *manager = nil;
        for (NETransparentProxyManager *candidate in managers) {
            if ([candidate.protocolConfiguration isKindOfClass:NETunnelProviderProtocol.class] &&
                [((NETunnelProviderProtocol *)candidate.protocolConfiguration).providerBundleIdentifier isEqual:identifier]) {
                manager = candidate;
                break;
            }
        }
        if (manager == nil) manager = [[[NETransparentProxyManager alloc] init] autorelease];
        if (manager.connection.status == NEVPNStatusConnected || manager.connection.status == NEVPNStatusConnecting) {
            fprintf(stderr, "另一个 Lowpath 路由会话仍在运行。\n");
            exit(1);
        }
        NETunnelProviderProtocol *configuration = [[[NETunnelProviderProtocol alloc] init] autorelease];
        configuration.providerBundleIdentifier = identifier;
        configuration.serverAddress = @"Lowpath";
        manager.protocolConfiguration = configuration;
        manager.localizedDescription = @"Lowpath 游戏路由";
        manager.enabled = YES;
        [manager saveToPreferencesWithCompletionHandler:^(NSError *error) {
            if (error != nil) controlError(error);
            [manager loadFromPreferencesWithCompletionHandler:^(NSError *loadError) {
                if (loadError != nil) controlError(loadError);
                NEVPNConnection *connection = manager.connection;
                if (![connection isKindOfClass:NETunnelProviderSession.class]) {
                    fprintf(stderr, "未找到 Lowpath 网络扩展会话。\n");
                    exit(1);
                }
                NSError *startError = nil;
                NETunnelProviderSession *session = (NETunnelProviderSession *)connection;
                if (![session startTunnelWithOptions:@{@"bridge": self.bridge} andReturnError:&startError]) {
                    controlError(startError);
                }
            }];
        }];
    }];
}
@end

void lowpath_proxy_control(const char *json) {
    @autoreleasepool {
        if (!lowpath_proxy_embedded()) return;
        LowpathExtensionRequest *delegate = [[LowpathExtensionRequest alloc] init];
        delegate.bridge = [NSString stringWithUTF8String:json];
        OSSystemExtensionRequest *request = [OSSystemExtensionRequest
            activationRequestForExtension:providerIdentifier() queue:dispatch_get_main_queue()];
        request.delegate = delegate;
        [OSSystemExtensionManager.sharedManager submitRequest:request];
        CFRunLoopRun();
        [delegate release];
    }
}
