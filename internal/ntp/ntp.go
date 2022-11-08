package ntp

import (
	"errors"
	"fmt"
	"github.com/ajanata/textbuf"
	"machine"
	"runtime"
	"time"
	"tinygo.org/x/drivers/net"
	"tinygo.org/x/drivers/wifinina"
)

const ntpPacketSize = 48

var b = make([]byte, ntpPacketSize)

// NTP connects to the given Wi-Fi network and sends an NTP request to the given host. If no errors occur, and a
// response is received, the time offset is adjusted so that the current time returned by time.Now() is approximately
// correct.
//
// TODO: pass in the Nina coprocessor device so we don't have to rely on the machine package providing it?
//
// based on https://github.com/tinygo-org/drivers/blob/release/examples/wifinina/ntpclient/main.go
func NTP(ntpHost, wifiSSID, wifiPassword string, tzOffset time.Duration, buf *textbuf.Buffer) error {
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
