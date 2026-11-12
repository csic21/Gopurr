package chart

import "gopurr/internal/appui"

type Curve int

const CurveMonotone Curve = 1

func Area(args ...any) appui.Element { return appui.Element{} }
func Line(args ...any) appui.Element { return appui.Element{} }
func Y(args ...any) float32          { return 0 }
