module github.com/ajanata/gotogen-hardware

go 1.19

// these replace directives make my development life easier. go.work wasn't working for me.
replace (
	github.com/ajanata/gotogen => ../gotogen
	github.com/ajanata/oled_font => ../oled_font
	github.com/ajanata/textbuf => ../textbuf
	tinygo.org/x/drivers => ../tinygo-drivers
)

// fixes compile error for apds9960, and merges rgb75 driver for the LED panels.
// you will need to leave this one here!
//replace tinygo.org/x/drivers => github.com/ajanata/tinygo-drivers v0.0.0-20221017002437-9bf48ad71415

// hacks to make it work with the matrixportal-m4
//replace github.com/aykevl/things => github.com/ajanata/aykevl-things v0.0.0-20221022221256-2372aa753afb

replace github.com/aykevl/things => ../aykevl-things

require (
	github.com/ajanata/gotogen v0.0.0-20221016220840-b3704754d9ad
	github.com/ajanata/textbuf v0.0.2
	github.com/aykevl/things v0.0.0-20221017191438-a010d20916fe
	tinygo.org/x/drivers v0.23.0
)

require (
	github.com/ajanata/oled_font v1.2.0 // indirect
	golang.org/x/image v0.0.0-20210628002857-a66eb6448b8d // indirect
)
