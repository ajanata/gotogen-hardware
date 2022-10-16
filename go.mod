module github.com/ajanata/gotogen-hardware

go 1.19

// these replace directives make my development life easier. go.work wasn't working for me.
replace (
	github.com/ajanata/gotogen => ../gotogen
	github.com/ajanata/oled_font => ../oled_font
	github.com/ajanata/textbuf => ../textbuf
)

// fixes compile error for apds9960. you will need to leave this one here!
replace tinygo.org/x/drivers => github.com/ajanata/tinygo-drivers v0.0.0-20221010064956-016cdce8a129

require (
	github.com/ajanata/gotogen v0.0.0-20221016220840-b3704754d9ad
	tinygo.org/x/drivers v0.23.0
)

require (
	github.com/ajanata/oled_font v1.2.0 // indirect
	github.com/ajanata/textbuf v0.0.2 // indirect
	golang.org/x/image v0.0.0-20210628002857-a66eb6448b8d // indirect
)
