package main

import (
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/cj123/assetto-server-manager"
	"github.com/cj123/assetto-server-manager/cmd/server-manager/static"
	"github.com/cj123/assetto-server-manager/cmd/server-manager/views"
	"github.com/cj123/assetto-server-manager/pkg/udp"
	"github.com/cj123/assetto-server-manager/pkg/udp/replay"

	"github.com/etcd-io/bbolt"
	"github.com/pkg/browser"
	"github.com/sirupsen/logrus"
)

var (
	defaultAddress = "0.0.0.0:8772"
)

const (
	udpRealtimePosRefreshIntervalMin = 100
)

func init() {
	runtime.LockOSThread()
	servermanager.InitLogging()
}

func main() {
	config, err := servermanager.ReadConfig("config.yml")

	if err != nil {
		ServeHTTPWithError(defaultAddress, "Read configuration file (config.yml)", err)
		return
	}

	if config.Monitoring.Enabled {
		servermanager.InitMonitoring()
	}

	store, err := config.Store.BuildStore()

	if err != nil {
		ServeHTTPWithError(config.HTTP.Hostname, "Open server manager storage (bolt or json)", err)
		return
	}

	var templateLoader servermanager.TemplateLoader
	var filesystem http.FileSystem

	if os.Getenv("FILESYSTEM_HTML") == "true" {
		templateLoader = servermanager.NewFilesystemTemplateLoader("views")
		filesystem = http.Dir("static")
	} else {
		templateLoader = &views.TemplateLoader{}
		filesystem = static.FS(false)
	}

	resolver, err := servermanager.NewResolver(templateLoader, os.Getenv("FILESYSTEM_HTML") == "true", store)

	if err != nil {
		ServeHTTPWithError(config.HTTP.Hostname, "Initialise resolver (internal error)", err)
		return
	}
	servermanager.SetAssettoInstallPath(config.Steam.InstallPath)

	err = servermanager.InstallAssettoCorsaServer(config.Steam.Username, config.Steam.Password, config.Steam.ForceUpdate)

	if err != nil {
		ServeHTTPWithError(defaultAddress, "Install assetto corsa server with steamcmd. Likely you do not have steamcmd installed correctly.", err)
		return
	}

	err = servermanager.InitWithResolver(resolver)

	if err != nil {
		ServeHTTPWithError(config.HTTP.Hostname, "Initialise server manager (internal error)", err)
		return
	}

	if config.LiveMap.IsEnabled() {
		if config.LiveMap.IntervalMs < udpRealtimePosRefreshIntervalMin {
			udp.RealtimePosIntervalMs = udpRealtimePosRefreshIntervalMin
		} else {
			udp.RealtimePosIntervalMs = config.LiveMap.IntervalMs
		}
	}

	listener, err := net.Listen("tcp", config.HTTP.Hostname)

	if err != nil {
		ServeHTTPWithError(defaultAddress, "Listen on hostname "+config.HTTP.Hostname+". Likely the port has already been taken by another application", err)
		return
	}

	logrus.Infof("starting assetto server manager on: %s", config.HTTP.Hostname)

	if runtime.GOOS == "windows" {
		_ = browser.OpenURL("http://" + strings.Replace(config.HTTP.Hostname, "0.0.0.0", "127.0.0.1", 1))
	}

	router := resolver.ResolveRouter(filesystem)

	if err := http.Serve(listener, router); err != nil {
		logrus.Fatal(err)
	}
}

func startUDPReplay(resolver *servermanager.Resolver, file string) {
	time.Sleep(time.Second * 20)

	db, err := bbolt.Open(file, 0644, nil)

	if err != nil {
		logrus.WithError(err).Error("Could not open bolt")
	}

	err = replay.ReplayUDPMessages(db, 1, func(message udp.Message) {
		resolver.UDPCallback(message)
	}, time.Millisecond*500)

	if err != nil {
		logrus.WithError(err).Error("UDP Replay failed")
	}
}
