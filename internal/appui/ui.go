// Package appui is the retired desktop widget surface. The native host draws
// the window and tray; these types keep the old view code compiling without
// the previous toolkit.
package appui

import (
	"image"
	"image/color"
	"time"
)

type Color struct{ R, G, B, A uint8 }

func Hex(string) Color                    { return Color{A: 255} }
func RGB(uint8, uint8, uint8) Color       { return Color{A: 255} }
func RGBA(r, g, b uint8, a float32) Color { return Color{R: r, G: g, B: b, A: uint8(a * 255)} }

type Rect struct{ X, Y, W, H float32 }

type Key int

const (
	KeyEscape Key = iota + 1
	KeyEnter
	KeySpace
	KeyBackslash
	KeyDown
	KeyRight
)

type Modifiers int

const (
	Cmd Modifiers = 1 << iota
)

type Direction int

const (
	LeftToRight Direction = iota
	RightToLeft
)

type Align int

const (
	Center Align = iota + 1
	Start
	End
	Stretch
	Baseline
)

type Role int

const (
	RoleNone Role = iota
	RoleButton
	RoleDialog
	RoleAlertDialog
	RoleRadio
	RoleStatus
)

type Cursor int

const CursorPointer Cursor = 1

type Element struct{}

func (e Element) element() Element { return e }

type Context struct{}

func (c *Context) Invalidate() {}

type ListState struct{ KeepOffset bool }
type ScrollState struct{ Y, MaxY float32 }
type Services struct{}

func (s Services) Invalidate() {}

type SVG struct{}
type Bitmap struct{ W, H int }

func (b *Bitmap) Size() (int, int) {
	if b == nil {
		return 0, 0
	}
	return b.W, b.H
}

func DecodeBitmap([]byte) (*Bitmap, error) { return &Bitmap{}, nil }
func MustParseSVG(any) *SVG                { return &SVG{} }

type Theme struct {
	Background, Surface, SurfaceHover, SurfacePressed, Border       Color
	Text, TextMuted, Accent, AccentHover, AccentPressed, AccentText Color
	Danger, Warning, Success, Selection, Focus, Scrollbar           Color
	Dark                                                            bool
	Radius, Spacing, FontSize, ScrollbarWidth                       float32
	Font                                                            string
}

func (t *Theme) BackgroundColor() Color { return t.Background }

type Preferences struct {
	ReduceMotion bool
	TextScale    float32
}

type TitleBar struct{ Height, Left, Right float32 }

type Painter struct{}

func (p *Painter) Fill(Rect, Color, float32) {}
func (p *Painter) Theme() *Theme             { return &Theme{} }

type Tester struct{}

func NewTester(view func(*Context), size ...any) *Tester { return &Tester{} }

func (t *Tester) SetPreferences(Preferences)                {}
func (t *Tester) SetTitleBar(TitleBar)                      {}
func (t *Tester) SetLocale(string)                          {}
func (t *Tester) SetDark(bool)                              {}
func (t *Tester) Advance(time.Duration)                     {}
func (t *Tester) Frame()                                    {}
func (t *Tester) Click(string) error                        { return nil }
func (t *Tester) Find(string) (Rect, bool)                  { return Rect{}, false }
func (t *Tester) HasText(string) bool                       { return false }
func (t *Tester) Scroll(float32, float32, float32, float32) {}
func (t *Tester) Press(float32, float32)                    {}
func (t *Tester) Release(float32, float32)                  {}
func (t *Tester) Move(float32, float32)                     {}

type FrameImage struct{ Pix []byte }

func (f *FrameImage) RGBAAt(int, int) color.RGBA { return color.RGBA{A: 255} }
func (f *FrameImage) At(int, int) color.Color    { return color.RGBA{A: 255} }
func (f *FrameImage) Bounds() image.Rectangle    { return image.Rect(0, 0, 1060, 720) }
func (f *FrameImage) ColorModel() color.Model    { return color.RGBAModel }
func (t *Tester) Image() *FrameImage             { return &FrameImage{} }

type Easing func(float32) float32

func EaseOut(float32) float32 { return 0 }
func Smooth(float32) float32  { return 0 }
func CubicBezier(float32, float32, float32, float32) Easing {
	return func(float32) float32 { return 0 }
}

type SpringOptions struct{ Duration time.Duration }

type Animation struct {
	Duration time.Duration
	Ease     Easing
}

type Motion struct{ X, Y, Opacity float32 }

type ElementTransition struct {
	Duration  time.Duration
	Animation *Animation
	Colors    bool
	Enter     *Motion
	Exit      *Motion
}

type ContentTransition struct{ Animation *Animation }

func (c Color) Alpha(float32) Color { return c }

type theme struct{}

func LightTheme() *Theme { return &Theme{} }
func DarkTheme() *Theme  { return &Theme{} }

func (c *Context) Size(args ...any) (float32, float32) { return 1060, 720 }

func (c *Context) Key(args ...any) *Context {
	if c == nil {
		return &Context{}
	}
	return c
}

func Overlay(args ...any) Element { return Element{} }

func Box(args ...any) Element { return Element{} }

func Column(args ...any) Element { return Element{} }

func Text(args ...any) Element { return Element{} }

func Row(args ...any) Element { return Element{} }

func TextInput(args ...any) Element { return Element{} }

func (e Element) Absolute(args ...any) Element {
	return Element{}
}

func (e Element) Width(args ...any) Element {
	return Element{}
}

func (e Element) FontSize(args ...any) Element {
	return Element{}
}

func (e Element) FillWidth(args ...any) Element {
	return Element{}
}

func (e Element) Grow(args ...any) Element {
	return Element{}
}

func (e Element) AlignSelf(args ...any) Element {
	return Element{}
}

func Scroll(args ...any) Element { return Element{} }

func (e Element) Left(args ...any) Element {
	return Element{}
}

func (e Element) MaxHeight(args ...any) Element {
	return Element{}
}

func (e Element) FontWeight(args ...any) Element {
	return Element{}
}

func (e Element) TextColor(args ...any) Element {
	return Element{}
}

func (e Element) Gap(args ...any) Element {
	return Element{}
}

func (e Element) MinWidth(args ...any) Element {
	return Element{}
}

func (e Element) Label(args ...any) Element {
	return Element{}
}

func (e Element) Shrink(args ...any) Element {
	return Element{}
}

func (e Element) Top(args ...any) Element {
	return Element{}
}

func (e Element) Padding(args ...any) Element {
	return Element{}
}

func (e Element) AlignItems(args ...any) Element {
	return Element{}
}

func (e Element) Placeholder(args ...any) Element {
	return Element{}
}

func (e Element) Disabled(args ...any) Element {
	return Element{}
}

func (e Element) MinHeight(args ...any) Element {
	return Element{}
}

func (e Element) Children(args ...any) Element {
	return Element{}
}

func Checkbox(args ...any) Element { return Element{} }

func (e Element) Right(args ...any) Element {
	return Element{}
}

func (e Element) Radius(args ...any) Element {
	return Element{}
}

func (e Element) OnChange(args ...any) Element {
	return Element{}
}

func (e Element) Selectable(args ...any) Element {
	return Element{}
}

func (e Element) OnClick(args ...any) Element {
	return Element{}
}

func Spacer(args ...any) Element { return Element{} }

func (e Element) Bottom(args ...any) Element {
	return Element{}
}

func (e Element) Background(args ...any) Element {
	return Element{}
}

func (e Element) PaddingY(args ...any) Element {
	return Element{}
}

func (e Element) MaxLines(args ...any) Element {
	return Element{}
}

func (c *Context) SetTheme(args ...any) *Context {
	return &Context{}
}

func (c *Context) Theme(args ...any) *Theme { return &Theme{} }

func (c *Context) Preferences(args ...any) Preferences { return Preferences{} }

func (c *Context) DisableAnimations(args ...any) *Context {
	return &Context{}
}

func (e Element) Fill(args ...any) Element {
	return Element{}
}

func (e Element) Center(args ...any) Element {
	return Element{}
}

func (e Element) Border(args ...any) Element {
	return Element{}
}

func (e Element) Ellipsis(args ...any) Element {
	return Element{}
}

func (c *Context) Dark(args ...any) *Context {
	return &Context{}
}

func (c *Context) ReduceMotion(args ...any) *Context {
	return &Context{}
}

func (e Element) Justify(args ...any) Element {
	return Element{}
}

func (e Element) Bold(args ...any) Element {
	return Element{}
}

func (c *Context) Services(args ...any) Services { return Services{} }

func (e Element) FillHeight(args ...any) Element {
	return Element{}
}

func (e Element) Modal(args ...any) Element {
	return Element{}
}

func (e Element) Role(args ...any) Element {
	return Element{}
}

func (e Element) Tooltip(args ...any) Element {
	return Element{}
}

func (e Element) Translate(args ...any) Element {
	return Element{}
}

func (e Element) Opacity(args ...any) Element {
	return Element{}
}

func Image(args ...any) Element { return Element{} }

func (e Element) OverlayShortcut(args ...any) bool { return false }

func (e Element) BorderWidth(args ...any) Element {
	return Element{}
}

func (e Element) Size(args ...any) Element {
	return Element{}
}

func (e Element) PaddingX(args ...any) Element {
	return Element{}
}

func (e Element) Key(args ...any) Element {
	return Element{}
}

func (e Element) BorderColor(args ...any) Element {
	return Element{}
}

func (e Element) Transition(args ...any) Element {
	return Element{}
}

func (e Element) Height(args ...any) Element {
	return Element{}
}

func (e Element) SingleLine(args ...any) Element {
	return Element{}
}

func (e Element) AnimateTo(args ...any) float32 { return 0 }

func (e Element) Draw(args ...any) Element {
	return Element{}
}

func (p *Painter) Clip(args ...any) {}

func (p *Painter) Line(args ...any) {}

func (p *Painter) FillPathGradient(args ...any) {}

func (p *Painter) StrokePath(args ...any) {}

type LinearGradient struct {
	From, To Color
	Angle    float32
}

func (p *Painter) StrokePathGradient(args ...any) {}

func (p *Painter) Shadow(args ...any) {}

func TextArea(args ...any) Element { return Element{} }

func (e Element) Font(args ...any) Element {
	return Element{}
}

func (e Element) ReadOnly(args ...any) Element {
	return Element{}
}

func (s Services) WriteClipboard(args ...any) {}

func List(args ...any) Element { return Element{} }

func (e Element) ItemKey(args ...any) Element {
	return Element{}
}

func (e Element) Rows(args ...any) Element {
	return Element{}
}

type ListRow struct {
	Context *Context
	Index   int
}

func (r ListRow) Hovered(args ...any) bool { return false }

func (e Element) Hovered(args ...any) bool  { return false }
func (e Element) Pressed(args ...any) bool  { return false }
func (e Element) Focused(args ...any) bool  { return false }
func (e Element) Selected(args ...any) bool { return false }

func (e Element) TextAlign(args ...any) Element {
	return Element{}
}

func (e Element) FontFeatures(args ...any) Element {
	return Element{}
}

func (e Element) MaxWidth(args ...any) Element {
	return Element{}
}

func Select(args ...any) Element { return Element{} }

func (e Element) Changed(args ...any) bool { return false }

func (c *Context) Now(args ...any) time.Time { return time.Now() }

func (c *Context) After(args ...any) *Context {
	return &Context{}
}

func ButtonBase(args ...any) Element { return Element{} }

var Transparent Color

func Icon(args ...any) Element { return Element{} }

func (c *Context) Add(args ...any) *Context {
	return &Context{}
}

func (c *Context) Before(args ...any) *Context {
	return &Context{}
}

func (e Element) IsDisabled(args ...any) bool { return false }

func (e Element) FocusVisible(args ...any) bool { return false }

func (e Element) Checked(args ...any) Element { return e }

func (c *Context) WriteClipboard(args ...any) error {
	return nil
}

func (c *Context) OpenURL(args ...any) error {
	return nil
}

func (c *Context) OpenURLThen(args ...any) {}

func (th *Theme) Space(n float32) float32 {
	if th == nil || th.Spacing <= 0 {
		return n * 4
	}
	return n * th.Spacing
}

func (col Color) Mix(args ...any) Color {
	return Color{}
}

func (c *Context) Direction(args ...any) Direction {
	return LeftToRight
}

func Button(args ...any) Element { return Element{} }

func PrimaryButton(args ...any) Element { return Element{} }

func SwitchBase(args ...any) Element { return Element{} }

type SegmentedParts struct{ Track Element }

func SegmentedBase(args ...any) SegmentedParts       { return SegmentedParts{Track: Element{}} }
func (p SegmentedParts) Segment(args ...any) Element { return Element{} }

func (e Element) Spring(args ...any) float32 {
	return 0
}

func (e Element) Track(args ...any) Element {
	return Element{}
}

func (c *Context) MeasureText(args ...any) (float32, float32) {
	return 0, 0
}

type Span struct {
	Text   string
	Size   float32
	Weight int
	Color  Color
}

func (e Element) Segment(args ...any) Element {
	return Element{}
}

func (e Element) Shadow(args ...any) Element {
	return Element{}
}

func (e Element) Scale(args ...any) Element {
	return Element{}
}

func (e Element) Description(args ...any) Element {
	return Element{}
}

func (e Element) Margin(args ...any) Element {
	return Element{}
}

func (e Element) Clicked(args ...any) bool { return false }

func Sidebar(args ...any) Element { return Element{} }

func SidebarItem(args ...any) Element { return Element{} }

func Local(args ...any) *bool { v := false; return &v }

func DialogBase(args ...any) Element { return Element{} }

const AlertDismissed = -1

func (e Element) AutoFocus(args ...any) Element {
	return Element{}
}

func (c *Context) OnShortcut(args ...any) {}

func (e Element) Clip(args ...any) Element {
	return Element{}
}

func (c *Context) TitleBar(args ...any) TitleBar {
	return TitleBar{}
}

func (e Element) DragWindow(args ...any) Element {
	return Element{}
}

func (p *Painter) FillGradient(args ...any) {}

func (e Element) ContentTransition(args ...any) Element {
	return Element{}
}

func (s ScrollState) ScrollTo(args ...any) Element {
	return Element{}
}

func (e Element) TrackScroll(args ...any) Element {
	return Element{}
}

func (e Element) Cursor(args ...any) Element {
	return Element{}
}

func (e Element) Focusable(args ...any) Element {
	return Element{}
}

func Progress(args ...any) Element { return Element{} }

func (e Element) Password(args ...any) Element {
	return Element{}
}

func (e Element) OnSubmit(args ...any) Element {
	return Element{}
}

func (e Element) MarginX(args ...any) Element {
	return Element{}
}

func TextInputBase(args ...any) Element { return Element{} }

func (t *Tester) ClickAt(args ...any) error { return nil }
func (t *Tester) Type(args ...any) error    { return nil }
func (t *Tester) Command(args ...any) error { return nil }
func (t *Tester) Key(args ...any) error     { return nil }
func (t *Tester) Drag(args ...any) error    { return nil }
func (t *Tester) ScrollAt(args ...any)      {}
func (t *Tester) Text() string              { return "" }

func (t *Tester) Clipboard() string             { return "" }
func (t *Tester) OpenedURLs() []string          { return nil }
func (t *Tester) FailOpenURL(args ...any)       {}
func (t *Tester) SetClipboard(args ...any)      {}
func (t *Tester) Hover(args ...any)             {}
func (t *Tester) RightClick(args ...any) error  { return nil }
func (t *Tester) DoubleClick(args ...any) error { return nil }

func (t *Tester) Focused(string) bool { return false }

func (t *Tester) Texts() []string { return nil }

func (t *Tester) FramePending() bool { return false }

func (t *Tester) SetSize(args ...any)      {}
func (t *Tester) SetDirection(args ...any) {}
func (t *Tester) Cursor() Cursor           { return 0 }
