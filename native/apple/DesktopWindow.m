#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <TargetConditionals.h>
#import <objc/message.h>
#import <objc/runtime.h>
#import "DesktopWindow.h"

#if TARGET_OS_MACCATALYST
static id application(void) {
    Class cls = NSClassFromString(@"NSApplication");
    return cls ? ((id(*)(id,SEL))objc_msgSend)(cls, NSSelectorFromString(@"sharedApplication")) : nil;
}
static void sendObject(id target, NSString *name, id object) {
    ((void(*)(id,SEL,id))objc_msgSend)(target, NSSelectorFromString(name), object);
}
static __weak id mainWindow;
static const void *hookKey = &hookKey;
static const void *applicationHookKey = &applicationHookKey;

static CGRect frameOf(id view) {
    return ((CGRect(*)(id,SEL))objc_msgSend)(view,NSSelectorFromString(@"frame"));
}
static void layoutChrome(id window) {
    // Match the established desktop's TrafficLightPosition{20,12}; its
    // 14-point buttons produce the same 38-point title bar used by NSH.
    NSUInteger style = ((NSUInteger(*)(id,SEL))objc_msgSend)(window,NSSelectorFromString(@"styleMask"));
    if (style & (1UL << 14)) return; // NSWindowStyleMaskFullScreen
    id close = ((id(*)(id,SEL,NSInteger))objc_msgSend)(window,NSSelectorFromString(@"standardWindowButton:"),0);
    id mini = ((id(*)(id,SEL,NSInteger))objc_msgSend)(window,NSSelectorFromString(@"standardWindowButton:"),1);
    if (!close || !mini) return;
    id parent = ((id(*)(id,SEL))objc_msgSend)(close,NSSelectorFromString(@"superview"));
    id container = ((id(*)(id,SEL))objc_msgSend)(parent,NSSelectorFromString(@"superview"));
    if (!container) return;
    CGRect button = frameOf(close), other = frameOf(mini), box = frameOf(container);
    CGFloat height = button.size.height + 24;
    CGRect desired = box; desired.size.height = height; desired.origin.y = frameOf(window).size.height-height;
    if (!CGRectEqualToRect(box,desired)) ((void(*)(id,SEL,CGRect))objc_msgSend)(container,NSSelectorFromString(@"setFrame:"),desired);
    CGFloat gap = other.origin.x-button.origin.x;
    for (NSInteger i=0;i<3;i++) {
        id control = ((id(*)(id,SEL,NSInteger))objc_msgSend)(window,NSSelectorFromString(@"standardWindowButton:"),i);
        if (!control) continue;
        CGPoint origin = CGPointMake(20+i*gap,12);
        if (!CGPointEqualToPoint(frameOf(control).origin,origin)) ((void(*)(id,SEL,CGPoint))objc_msgSend)(control,NSSelectorFromString(@"setFrameOrigin:"),origin);
    }
}

@interface GopurrApplicationDelegate : NSProxy
@property(nonatomic, strong) id target;
@property(nonatomic, copy) void (^visibility)(int);
@end
@implementation GopurrApplicationDelegate
- (BOOL)respondsToSelector:(SEL)selector {
    return selector == @selector(applicationShouldHandleReopen:hasVisibleWindows:) || selector == @selector(applicationShouldTerminate:) || [self.target respondsToSelector:selector];
}
- (NSMethodSignature *)methodSignatureForSelector:(SEL)selector { return [self.target methodSignatureForSelector:selector]; }
- (void)forwardInvocation:(NSInvocation *)invocation { [invocation invokeWithTarget:self.target]; }
- (BOOL)applicationShouldHandleReopen:(id)sender hasVisibleWindows:(BOOL)visible {
    if (self.visibility) self.visibility(1);
    GopurrDesktopShowWindow();
    return YES;
}
- (NSInteger)applicationShouldTerminate:(id)sender {
    // NSTerminateCancel: Swift asks the backend to restore proxy state first.
    // Only a successful shutdown may exit the frontend.
    if (self.visibility) self.visibility(-1);
    return 0;
}
@end

// Forward every UIKit window delegate method, intercepting only close/minimize.
// Hiding preserves the NSH tree and unsaved editors until light-mode releases it.
@interface GopurrWindowDelegate : NSProxy
@property(nonatomic, strong) id target;
@property(nonatomic, copy) void (^visibility)(int);
@end
@implementation GopurrWindowDelegate
- (BOOL)respondsToSelector:(SEL)selector {
    return selector == @selector(windowShouldClose:) || selector == @selector(windowDidMiniaturize:) || selector == @selector(windowDidDeminiaturize:) || selector == @selector(windowDidResize:) || selector == @selector(windowDidEnterFullScreen:) || selector == @selector(windowDidExitFullScreen:) || [self.target respondsToSelector:selector];
}
- (NSMethodSignature *)methodSignatureForSelector:(SEL)selector { return [self.target methodSignatureForSelector:selector]; }
- (void)forwardInvocation:(NSInvocation *)invocation { [invocation invokeWithTarget:self.target]; }
- (BOOL)windowShouldClose:(id)window {
    // The first close needs the same explicit background-running explanation
    // as the original desktop window. Swift/NSH decides when to hide it.
    if (self.visibility) self.visibility(2);
    return NO;
}
- (void)windowDidMiniaturize:(NSNotification *)notification {
    if (self.visibility) self.visibility(0);
    if ([self.target respondsToSelector:_cmd]) sendObject(self.target, NSStringFromSelector(_cmd), notification);
}
- (void)windowDidDeminiaturize:(NSNotification *)notification {
    if ([self.target respondsToSelector:_cmd]) sendObject(self.target, NSStringFromSelector(_cmd), notification);
    if (self.visibility) self.visibility(1);
}
- (void)windowDidResize:(NSNotification *)notification {
    if ([self.target respondsToSelector:_cmd]) sendObject(self.target,NSStringFromSelector(_cmd),notification);
    layoutChrome(notification.object);
    if (self.visibility) self.visibility(3);
}
- (void)windowDidEnterFullScreen:(NSNotification *)notification {
    if ([self.target respondsToSelector:_cmd]) sendObject(self.target,NSStringFromSelector(_cmd),notification);
    if (self.visibility) self.visibility(3);
}
- (void)windowDidExitFullScreen:(NSNotification *)notification {
    if ([self.target respondsToSelector:_cmd]) sendObject(self.target,NSStringFromSelector(_cmd),notification);
    layoutChrome(notification.object);
    if (self.visibility) self.visibility(3);
}
@end

void GopurrDesktopSetDock(int visible) {
    id app = application();
    SEL sel = NSSelectorFromString(@"setActivationPolicy:");
    if ([app respondsToSelector:sel]) ((BOOL(*)(id,SEL,NSInteger))objc_msgSend)(app,sel,visible ? 0 : 1);
}
int GopurrDesktopInstallWindowHooks(void (^visibility)(int)) {
    id app = application();
    id window = mainWindow;
    if (!window) window = ((id(*)(id,SEL))objc_msgSend)(app,NSSelectorFromString(@"keyWindow"));
    // UIApplication may appear before AppKit has a key window, especially
    // when a desktop app launches in the background. Its visible main window
    // still exists and must receive close-to-tray and chrome hooks.
    if (!window) {
        NSArray *windows = ((id(*)(id,SEL))objc_msgSend)(app,NSSelectorFromString(@"windows"));
        for (id candidate in windows) {
            BOOL visible = ((BOOL(*)(id,SEL))objc_msgSend)(candidate,NSSelectorFromString(@"isVisible"));
            id close = ((id(*)(id,SEL,NSInteger))objc_msgSend)(candidate,NSSelectorFromString(@"standardWindowButton:"),0);
            if (visible && close) { window = candidate; break; }
        }
    }
    if (!window) return 0;
    GopurrApplicationDelegate *applicationProxy = objc_getAssociatedObject(app, applicationHookKey);
    if (!applicationProxy) {
        id original = ((id(*)(id,SEL))objc_msgSend)(app,NSSelectorFromString(@"delegate"));
        if (original) {
            applicationProxy = [GopurrApplicationDelegate alloc];
            applicationProxy.target = original;
            objc_setAssociatedObject(app,applicationHookKey,applicationProxy,OBJC_ASSOCIATION_RETAIN_NONATOMIC);
            sendObject(app,@"setDelegate:",applicationProxy);
        }
    }
    applicationProxy.visibility = visibility;
    mainWindow = window;
    layoutChrome(window);
    GopurrWindowDelegate *existing = objc_getAssociatedObject(window,hookKey);
    if (existing) { existing.visibility = visibility; return 1; }
    id original = ((id(*)(id,SEL))objc_msgSend)(window,NSSelectorFromString(@"delegate"));
    if (!original) return 0;
    GopurrWindowDelegate *proxy = [GopurrWindowDelegate alloc];
    proxy.target = original;
    proxy.visibility = visibility;
    objc_setAssociatedObject(window,hookKey,proxy,OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    sendObject(window,@"setDelegate:",proxy);
    return 1;
}
void GopurrDesktopShowWindow(void) {
    id window = mainWindow;
    if (!window) return;
    sendObject(window,@"deminiaturize:",nil);
    sendObject(window,@"makeKeyAndOrderFront:",nil);
    ((void(*)(id,SEL,BOOL))objc_msgSend)(application(),NSSelectorFromString(@"activateIgnoringOtherApps:"),YES);
}
void GopurrDesktopHideWindow(void) { if (mainWindow) sendObject(mainWindow,@"orderOut:",nil); }
void GopurrDesktopLayoutChrome(void) { if (mainWindow) layoutChrome(mainWindow); }
double GopurrDesktopTitlebarInset(void) {
    if (!mainWindow) return 80;
    NSUInteger style = ((NSUInteger(*)(id,SEL))objc_msgSend)(mainWindow,NSSelectorFromString(@"styleMask"));
    return style & (1UL << 14) ? 0 : 80;
}
#else
void GopurrDesktopSetDock(int visible) {}
int GopurrDesktopInstallWindowHooks(void (^visibility)(int)) { return 0; }
void GopurrDesktopShowWindow(void) {}
void GopurrDesktopHideWindow(void) {}
void GopurrDesktopLayoutChrome(void) {}
double GopurrDesktopTitlebarInset(void) { return 0; }
#endif
