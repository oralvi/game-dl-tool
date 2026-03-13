package main

import "fyne.io/fyne/v2"

const appIconSVG = `<svg width="256" height="256" viewBox="0 0 256 256" fill="none" xmlns="http://www.w3.org/2000/svg">
<rect x="20" y="20" width="216" height="216" rx="56" fill="#176768"/>
<path d="M76 86H180" stroke="#F8F4EC" stroke-width="20" stroke-linecap="round"/>
<path d="M92 128H164" stroke="#F8F4EC" stroke-width="20" stroke-linecap="round"/>
<path d="M108 170H148" stroke="#F2A03D" stroke-width="20" stroke-linecap="round"/>
</svg>
`

func appIconResource() fyne.Resource {
	return fyne.NewStaticResource("app-icon.svg", []byte(appIconSVG))
}
