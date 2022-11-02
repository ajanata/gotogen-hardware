//go:build matrixportal_m4

package main

import (
	"device/sam"
	"errors"
	"fmt"
	"image/color"
	"machine"
	"runtime"
	"time"

	"github.com/ajanata/gotogen"
	"github.com/ajanata/textbuf"
	"github.com/aykevl/things/hub75"
	"tinygo.org/x/drivers"
	"tinygo.org/x/drivers/net"
	"tinygo.org/x/drivers/ssd1306"
	"tinygo.org/x/drivers/wifinina"
	"tinygo.org/x/drivers/ws2812"
)

const ntpHost = "time.nist.gov"
const ntpPacketSize = 48

var b = make([]byte, ntpPacketSize)

// we're using SERCOM4 for SPI on the built-in matrix connector, so we have to define it ourselves
var matrixSPI = machine.SPI{
	Bus:    sam.SERCOM4_SPIM,
	SERCOM: 4,
}

// pins for SERCOM4
const (
	matrixSCK = machine.PB09
	matrixSDO = machine.PB08
	matrixSDI = machine.NoPin
)

type driver struct{}

func main() {
	time.Sleep(time.Second)
	blink()
	err := machine.I2C0.Configure(machine.I2CConfig{
		SCL:       machine.I2C0_SCL_PIN,
		SDA:       machine.I2C0_SDA_PIN,
		Frequency: 3.6 * machine.MHz,
	})
	if err != nil {
		earlyPanic(err)
	}
	blink()

	dev := ssd1306.NewI2C(machine.I2C0)
	dev.Configure(ssd1306.Config{Width: 128, Height: 64, Address: 0x3D, VccState: ssd1306.SWITCHCAPVCC})
	blink()
	dev.ClearBuffer()
	dev.ClearDisplay()
	blink()

	g, err := gotogen.New(60, nil, &dev, machine.LED, driver{})
	if err != nil {
		earlyPanic(err)
	}
	err = g.Init()
	if err != nil {
		earlyPanic(err)
	}

	g.Run()
}

func (driver) EarlyInit() (faceDisplay drivers.Displayer, menuInput gotogen.MenuInput, boopSensor gotogen.BoopSensor, err error) {
	// turn off the NeoPixel
	machine.NEOPIXEL.Configure(machine.PinConfig{Mode: machine.PinOutput})
	np := ws2812.New(machine.NEOPIXEL)
	_ = np.WriteColors([]color.RGBA{{}})

	err = matrixSPI.Configure(machine.SPIConfig{
		SDI:       matrixSDI,
		SDO:       matrixSDO,
		SCK:       matrixSCK,
		Frequency: 12 * machine.MHz,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	rgb := hub75.New(hub75.Config{
		Data:         matrixSDO,
		Clock:        matrixSCK,
		Latch:        machine.HUB75_LAT,
		OutputEnable: machine.HUB75_OE,
		A:            machine.PB00,
		B:            machine.PB02,
		C:            machine.PB03,
		D:            machine.PB05,
		Brightness:   0x1F,
		NumScreens:   4, // screens are 32x32 as far as this driver is concerned
	})

	return rgb, nil, nil, nil
}

func (driver) LateInit(buf *textbuf.Buffer) error {
	err := ntp(buf)
	if err != nil {
		return err
	}
	return nil
}

// based on https://github.com/tinygo-org/drivers/blob/release/examples/wifinina/ntpclient/main.go
func ntp(buf *textbuf.Buffer) error {
	_ = buf.Print("Wifi: init")

	err := machine.NINA_SPI.Configure(machine.SPIConfig{
		Frequency: 8 * machine.MHz,
		SDO:       machine.NINA_SDO,
		SDI:       machine.NINA_SDI,
		SCK:       machine.NINA_SCK,
	})
	if err != nil {
		return err
	}

	wifi := wifinina.New(machine.NINA_SPI,
		machine.NINA_CS,
		machine.NINA_ACK,
		machine.NINA_GPIO0,
		machine.NINA_RESETN)
	wifi.Configure()
	time.Sleep(1 * time.Second)

	_ = buf.Println(".\nConnect: " + wifiSSID)
	err = wifi.ConnectToAccessPoint(wifiSSID, wifiPassword, 10*time.Second)
	if err != nil {
		return err
	}

	_ = buf.Print("DHCP: ")
	time.Sleep(time.Second)
	myIP, _, _, err := wifi.GetIP()
	if err != nil {
		return err
	}
	_ = buf.Print(myIP.String() + "\nSetting time")

	// now make UDP connection
	ip := net.ParseIP(ntpHost)
	raddr := &net.UDPAddr{IP: ip, Port: 123}
	laddr := &net.UDPAddr{Port: 2390}
	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		return err
	}
	t, err := getCurrentTime(conn)
	if err != nil {
		return err
	}
	runtime.AdjustTimeOffset(-1*int64(time.Since(t)) + int64(tzOffset))
	_ = buf.Println(".")

	return nil
}

func getCurrentTime(conn *net.UDPSerialConn) (time.Time, error) {
	if err := sendNTPpacket(conn); err != nil {
		return time.Time{}, err
	}
	clearBuffer()
	for now := time.Now(); time.Since(now) < time.Second; {
		time.Sleep(5 * time.Millisecond)
		if n, err := conn.Read(b); err != nil {
			return time.Time{}, fmt.Errorf("error reading UDP packet: %w", err)
		} else if n == 0 {
			continue // no packet received yet
		} else if n != ntpPacketSize {
			if n != ntpPacketSize {
				return time.Time{}, fmt.Errorf("expected NTP packet size of %d: %d", ntpPacketSize, n)
			}
		}
		return parseNTPpacket(), nil
	}
	return time.Time{}, errors.New("no packet received after 1 second")
}

func clearBuffer() {
	for i := range b {
		b[i] = 0
	}
}

func sendNTPpacket(conn *net.UDPSerialConn) error {
	clearBuffer()
	b[0] = 0b11100011 // LI, Version, Mode
	b[1] = 0          // Stratum, or type of clock
	b[2] = 6          // Polling Interval
	b[3] = 0xEC       // Peer Clock Precision
	// 8 bytes of zero for Root Delay & Root Dispersion
	b[12] = 49
	b[13] = 0x4E
	b[14] = 49
	b[15] = 52
	if _, err := conn.Write(b); err != nil {
		return err
	}
	return nil
}

func parseNTPpacket() time.Time {
	// the timestamp starts at byte 40 of the received packet and is four bytes,
	// this is NTP time (seconds since Jan 1 1900):
	t := uint32(b[40])<<24 | uint32(b[41])<<16 | uint32(b[42])<<8 | uint32(b[43])
	const seventyYears = 2208988800
	return time.Unix(int64(t-seventyYears), 0)
}
