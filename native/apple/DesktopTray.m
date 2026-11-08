#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <TargetConditionals.h>
#import <objc/message.h>
#import "DesktopWindow.h"

#if TARGET_OS_MACCATALYST
static id traySend(id object, NSString *selector) {
    return ((id(*)(id,SEL))objc_msgSend)(object,NSSelectorFromString(selector));
}
static void traySet(id object, NSString *selector, id value) {
    ((void(*)(id,SEL,id))objc_msgSend)(object,NSSelectorFromString(selector),value);
}
// Declare AppKit initializers without linking UIKit against AppKit headers.
@interface NSObject (GopurrTrayInitializers)
- (id)initWithTitle:(NSString *)title action:(SEL)action keyEquivalent:(NSString *)key;
- (id)initWithContentsOfFile:(NSString *)path;
@end
@interface GopurrTrayTarget : NSObject
@property(nonatomic,strong) id item;
@property(nonatomic,copy) void (^action)(const char *);
@end
@implementation GopurrTrayTarget
- (void)clicked:(id)sender {
    id app=traySend(NSClassFromString(@"NSApplication"),@"sharedApplication");
    id event=traySend(app,@"currentEvent");
    NSUInteger type=((NSUInteger(*)(id,SEL))objc_msgSend)(event,NSSelectorFromString(@"type"));
    NSUInteger modifiers=((NSUInteger(*)(id,SEL))objc_msgSend)(event,NSSelectorFromString(@"modifierFlags"));
    if(self.action)self.action(type==4 || (modifiers & (1UL<<18)) ? "menu" : "show");
}
- (void)activate:(id)sender {
    NSString *command=traySend(sender,@"representedObject");
    if(self.action && [command isKindOfClass:NSString.class])self.action(command.UTF8String);
}
@end
static GopurrTrayTarget *trayTarget;

static id buildTrayMenu(NSArray *entries) {
    id menu=[NSClassFromString(@"NSMenu") new];
    ((void(*)(id,SEL,BOOL))objc_msgSend)(menu,NSSelectorFromString(@"setAutoenablesItems:"),NO);
    for(NSDictionary *entry in entries) {
        if(![entry isKindOfClass:NSDictionary.class])continue;
        id item;
        if([entry[@"type"] isEqual:@"separator"])item=traySend(NSClassFromString(@"NSMenuItem"),@"separatorItem");
        else {
            item=[[NSClassFromString(@"NSMenuItem") alloc] initWithTitle:entry[@"label"] ?: @"" action:@selector(activate:) keyEquivalent:@""];
            traySet(item,@"setTarget:",trayTarget);
            traySet(item,@"setRepresentedObject:",entry[@"command"]);
            traySet(item,@"setToolTip:",entry[@"toolTip"]);
            ((void(*)(id,SEL,BOOL))objc_msgSend)(item,NSSelectorFromString(@"setEnabled:"),![entry[@"disabled"] boolValue]);
            ((void(*)(id,SEL,NSInteger))objc_msgSend)(item,NSSelectorFromString(@"setState:"),[entry[@"checked"] boolValue]?1:0);
            if([entry[@"submenu"] isKindOfClass:NSArray.class])traySet(item,@"setSubmenu:",buildTrayMenu(entry[@"submenu"]));
        }
        traySet(menu,@"addItem:",item);
    }
    return menu;
}
int GopurrDesktopInstallTray(const char *imagePath, void (^action)(const char *)) {
    if(trayTarget)return 1;
    id bar=traySend(NSClassFromString(@"NSStatusBar"),@"systemStatusBar");
    id item=((id(*)(id,SEL,CGFloat))objc_msgSend)(bar,NSSelectorFromString(@"statusItemWithLength:"),-1);
    id image=[[NSClassFromString(@"NSImage") alloc] initWithContentsOfFile:[NSString stringWithUTF8String:imagePath]];
    if(!item || !image)return 0;
    ((void(*)(id,SEL,CGSize))objc_msgSend)(image,NSSelectorFromString(@"setSize:"),CGSizeMake(18,18));
    ((void(*)(id,SEL,BOOL))objc_msgSend)(image,NSSelectorFromString(@"setTemplate:"),YES);
    trayTarget=[GopurrTrayTarget new];trayTarget.item=item;trayTarget.action=action;
    id button=traySend(item,@"button");
    traySet(button,@"setImage:",image);traySet(button,@"setToolTip:",@"Gopurr · 左键显示窗口，右键打开菜单");
    traySet(button,@"setTarget:",trayTarget);
    ((void(*)(id,SEL,SEL))objc_msgSend)(button,NSSelectorFromString(@"setAction:"),@selector(clicked:));
    ((NSUInteger(*)(id,SEL,NSUInteger))objc_msgSend)(button,NSSelectorFromString(@"sendActionOn:"),(1UL<<2)|(1UL<<4));
    traySet(item,@"setAutosaveName:",@"Gopurr.Main");
    return 1;
}
void GopurrDesktopShowTrayMenu(const char *json) {
    if(!trayTarget)return;
    NSData *data=[[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    id entries=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if(![entries isKindOfClass:NSArray.class])return;
    id menu=buildTrayMenu(entries);
    traySet(trayTarget.item,@"setMenu:",menu);
    traySet(traySend(trayTarget.item,@"button"),@"performClick:",nil);
    traySet(trayTarget.item,@"setMenu:",nil);
}
int GopurrDesktopTrayVisible(void) {
    return trayTarget && ((BOOL(*)(id,SEL))objc_msgSend)(trayTarget.item,NSSelectorFromString(@"isVisible"));
}
#else
int GopurrDesktopInstallTray(const char *path, void (^action)(const char *)) { return 0; }
void GopurrDesktopShowTrayMenu(const char *json) {}
int GopurrDesktopTrayVisible(void) { return 0; }
#endif
