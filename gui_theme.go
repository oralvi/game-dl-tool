package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type dlTheme struct{}

func (dlTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0xF7, G: 0xF4, B: 0xEC, A: 0xFF}
	case theme.ColorNameButton, theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return color.NRGBA{R: 0x17, G: 0x67, B: 0x68, A: 0xFF}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0x21, G: 0x27, B: 0x27, A: 0xFF}
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0xFA, G: 0xF7, B: 0xF0, A: 0xFF}
	case theme.ColorNameInputBackground, theme.ColorNameMenuBackground:
		return color.NRGBA{R: 0xFF, G: 0xFC, B: 0xF7, A: 0xFF}
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 0xE9, G: 0xE3, B: 0xD7, A: 0xFF}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x72, G: 0x78, B: 0x78, A: 0xFF}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x17, G: 0x67, B: 0x68, A: 0x33}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 0xF2, G: 0xA0, B: 0x3D, A: 0x66}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 0x12, G: 0x57, B: 0x58, A: 0x33}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0x17, G: 0x67, B: 0x68, A: 0x1F}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (dlTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (dlTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (dlTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameText:
		return 14
	case theme.SizeNameInlineIcon:
		return 18
	case theme.SizeNamePadding:
		return 10
	}
	return theme.DefaultTheme().Size(name)
}
