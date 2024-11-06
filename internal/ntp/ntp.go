package ntp

import (
	"fmt"
	"io"
	"net"
	"runtime"
	"time"

	"github.com/ajanata/textbuf"
	"tinygo.org/x/drivers/netlink"
	"tinygo.org/x/drivers/netlink/probe"
)

const ntpPacketSize = 48

// NTP connects to the given Wi-Fi network and sends an NTP request to the given host. If no errors occur, and a
// response is received, the time offset is adjusted so that the current time returned by time.Now() is approximately
// correct.
//
// based on https://github.com/tinygo-org/drivers/blob/release/examples/net/ntpclient/main.go
func NTP(ntpHost, wifiSSID, wifiPassword string, buf *textbuf.Buffer) error {
	_ = buf.Print("Wifi: init")
	linker, dever := probe.Probe()
	time.Sleep(1 * time.Second)

	_ = buf.Println(".\nConnect: " + wifiSSID)
	err := linker.NetConnect(&netlink.ConnectParams{
		Ssid:           wifiSSID,
		Passphrase:     wifiPassword,
		AuthType:       netlink.AuthTypeWPA2,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		return err
	}

	_ = buf.Print("DHCP: ")
	time.Sleep(time.Second)
	myIP, err := dever.Addr()
	if err != nil {
		return err
	}
	_ = buf.Print(myIP.String() + "\nSetting time")

	conn, err := net.Dial("udp", ntpHost)
	if err != nil {
		return err
	}
	t, err := getCurrentTime(conn)
	if err != nil {
		return err
	}
	runtime.AdjustTimeOffset(-1 * int64(time.Since(t)))
	_ = buf.Println(".")

	_ = conn.Close()
	linker.NetDisconnect()
	linker = nil
	dever = nil

	return nil
}

func getCurrentTime(conn net.Conn) (time.Time, error) {
	if err := sendNTPpacket(conn); err != nil {
		return time.Time{}, err
	}

	response := make([]byte, ntpPacketSize)
	n, err := conn.Read(response)
	if err != nil && err != io.EOF {
		return time.Time{}, err
	}
	if n != ntpPacketSize {
		return time.Time{}, fmt.Errorf("expected NTP packet size of %d: %d", ntpPacketSize, n)
	}

	return parseNTPPacket(response), nil
}

func sendNTPpacket(conn net.Conn) error {
	var request = [48]byte{
		0xe3,
	}

	_, err := conn.Write(request[:])
	return err
}

func parseNTPPacket(r []byte) time.Time {
	// the timestamp starts at byte 40 of the received packet and is four bytes,
	// this is NTP time (seconds since Jan 1 1900):
	t := uint32(r[40])<<24 | uint32(r[41])<<16 | uint32(r[42])<<8 | uint32(r[43])
	const seventyYears = 2208988800
	return time.Unix(int64(t-seventyYears), 0)
}
