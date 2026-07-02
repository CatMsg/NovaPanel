//go:build linux

package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"github.com/CatMsg/NovaPanel/logger"
	"golang.org/x/sys/unix"
)

type masqueTun struct {
	name       string
	fd         int
	file       *os.File
	peerPrefix netip.Prefix
}

func newMasqueTun(tag string, peerPrefix netip.Prefix, mtu int) (*masqueTun, error) {
	if !peerPrefix.Addr().Is4() {
		return nil, fmt.Errorf("masque tun only supports IPv4 peer address currently: %s", peerPrefix)
	}
	if mtu <= 0 {
		mtu = 1380
	}

	name := masqueTunName(tag)
	file, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fd := int(file.Fd())

	if err := tunSetIFF(fd, name); err != nil {
		_ = file.Close()
		return nil, err
	}

	t := &masqueTun{name: name, fd: fd, file: file, peerPrefix: peerPrefix}
	if err := t.configure(mtu); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

func masqueTunName(tag string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tag))
	return fmt.Sprintf("npmq%x", h.Sum32())[:12]
}

func tunSetIFF(fd int, name string) error {
	var ifr [unix.IFNAMSIZ + 64]byte
	copy(ifr[:unix.IFNAMSIZ], []byte(name))
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = unix.IFF_TUN | unix.IFF_NO_PI
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func (t *masqueTun) configure(mtu int) error {
	localAddr := masqueTunLocalAddr(t.name)
	cmds := [][]string{
		{"ip", "link", "set", "dev", t.name, "mtu", fmt.Sprintf("%d", mtu)},
		{"ip", "addr", "replace", localAddr + "/32", "dev", t.name},
		{"ip", "link", "set", "dev", t.name, "up"},
		{"ip", "route", "replace", t.peerPrefix.String(), "dev", t.name},
	}
	for _, args := range cmds {
		if err := runMasqueTunCommand(args...); err != nil {
			return err
		}
	}
	if err := t.configureKernelForwarding(); err != nil {
		return err
	}

	if err := ensureMasqueTunIptablesRule(
		[]string{"iptables", "-C", "FORWARD", "-i", t.name, "-j", "ACCEPT"},
		[]string{"iptables", "-I", "FORWARD", "1", "-i", t.name, "-j", "ACCEPT"},
	); err != nil {
		return err
	}
	if err := ensureMasqueTunIptablesRule(
		[]string{"iptables", "-C", "FORWARD", "-o", t.name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		[]string{"iptables", "-I", "FORWARD", "2", "-o", t.name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
	); err != nil {
		return err
	}

	if err := runMasqueTunCommand("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", t.peerPrefix.String(), "!", "-o", t.name, "-j", "MASQUERADE"); err != nil {
		if err := runMasqueTunCommand("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", t.peerPrefix.String(), "!", "-o", t.name, "-j", "MASQUERADE"); err != nil {
			return err
		}
	}
	return nil
}

func (t *masqueTun) configureKernelForwarding() error {
	settings := map[string]string{
		"/proc/sys/net/ipv4/ip_forward":                    "1\n",
		"/proc/sys/net/ipv4/conf/all/rp_filter":            "0\n",
		"/proc/sys/net/ipv4/conf/default/rp_filter":        "0\n",
		"/proc/sys/net/ipv4/conf/" + t.name + "/rp_filter": "0\n",
	}
	for path, value := range settings {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			return fmt.Errorf("write %s failed: %w", path, err)
		}
	}
	return nil
}

func ensureMasqueTunIptablesRule(checkArgs []string, addArgs []string) error {
	if err := runMasqueTunCommand(checkArgs...); err == nil {
		return nil
	}
	return runMasqueTunCommand(addArgs...)
}

func masqueTunLocalAddr(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	sum := h.Sum32()
	return fmt.Sprintf("169.254.%d.%d", byte(sum>>8), byte(sum))
}

func runMasqueTunCommand(args ...string) error {
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (t *masqueTun) ReadPacket(ctx context.Context, buf []byte) (int, error) {
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		pfd := []unix.PollFd{{Fd: int32(t.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pfd, 1000)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return 0, err
		}
		if n == 0 {
			continue
		}
		if pfd[0].Revents&unix.POLLIN != 0 {
			return unix.Read(t.fd, buf)
		}
	}
}

func (t *masqueTun) WritePacket(packet []byte) error {
	_, err := unix.Write(t.fd, packet)
	return err
}

func (t *masqueTun) Close() error {
	for {
		err := runMasqueTunCommand("iptables", "-D", "FORWARD", "-i", t.name, "-j", "ACCEPT")
		if err != nil {
			break
		}
	}
	for {
		err := runMasqueTunCommand("iptables", "-D", "FORWARD", "-o", t.name, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
		if err != nil {
			break
		}
	}
	for {
		err := runMasqueTunCommand("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", t.peerPrefix.String(), "!", "-o", t.name, "-j", "MASQUERADE")
		if err != nil {
			break
		}
	}
	if err := runMasqueTunCommand("ip", "route", "del", t.peerPrefix.String(), "dev", t.name); err != nil {
		logger.Debug("masque tun route cleanup skipped: ", err)
	}
	if err := runMasqueTunCommand("ip", "link", "delete", t.name); err != nil {
		logger.Debug("masque tun link cleanup skipped: ", err)
	}
	if t.file != nil {
		return t.file.Close()
	}
	return nil
}
