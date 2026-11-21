#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

void *lowpath_app_icon(const char *path, int size, int *length) {
    @autoreleasepool {
        NSString *bundle = [NSString stringWithUTF8String:path];
        if (bundle == nil) return NULL;
        NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:bundle];
        NSRect rect = NSMakeRect(0, 0, size, size);
        CGImageRef source = [icon CGImageForProposedRect:&rect context:nil hints:nil];
        if (source == NULL) return NULL;
        CGColorSpaceRef color = CGColorSpaceCreateDeviceRGB();
        CGContextRef bitmap = CGBitmapContextCreate(NULL, size, size, 8, 0, color, (CGBitmapInfo)kCGImageAlphaPremultipliedLast);
        CGColorSpaceRelease(color);
        if (bitmap == NULL) return NULL;
        CGContextSetInterpolationQuality(bitmap, kCGInterpolationHigh);
        CGContextDrawImage(bitmap, NSRectToCGRect(rect), source);
        CGImageRef scaled = CGBitmapContextCreateImage(bitmap);
        CGContextRelease(bitmap);
        if (scaled == NULL) return NULL;
        NSBitmapImageRep *rep = [[[NSBitmapImageRep alloc] initWithCGImage:scaled] autorelease];
        CGImageRelease(scaled);
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        if (png.length == 0) return NULL;
        void *bytes = malloc(png.length);
        if (bytes == NULL) return NULL;
        memcpy(bytes, png.bytes, png.length);
        *length = (int)png.length;
        return bytes;
    }
}
