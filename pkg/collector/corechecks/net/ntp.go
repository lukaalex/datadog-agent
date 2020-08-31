// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2020 Datadog, Inc.

package net

import (
	"expvar"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/beevik/ntp"
	"gopkg.in/yaml.v2"

	"github.com/DataDog/datadog-agent/pkg/aggregator"
	"github.com/DataDog/datadog-agent/pkg/autodiscovery/integration"
	"github.com/DataDog/datadog-agent/pkg/collector/check"
	core "github.com/DataDog/datadog-agent/pkg/collector/corechecks"
	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/DataDog/datadog-agent/pkg/telemetry"
	"github.com/DataDog/datadog-agent/pkg/util/alibaba"
	"github.com/DataDog/datadog-agent/pkg/util/azure"
	"github.com/DataDog/datadog-agent/pkg/util/ec2"
	"github.com/DataDog/datadog-agent/pkg/util/ecs"
	"github.com/DataDog/datadog-agent/pkg/util/gce"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/DataDog/datadog-agent/pkg/util/tencent"
)

const (
	ntpCheckName                 = "ntp"
	defaultMinCollectionInterval = 900 // 15 minutes, to follow pool.ntp.org's guidelines on the query rate
)

var (
	ntpExpVar = expvar.NewFloat("ntpOffset")
	// for testing purpose
	ntpQuery = ntp.QueryWithOptions

	tlmNtpOffset = telemetry.NewGauge("check", "ntp_offset",
		nil, "Ntp offset")

	awsNTPHosts     = []string{"169.254.169.123"}
	gcpNTPHosts     = []string{"metadata.google.internal"}
	azureNTPHosts   = []string{"time.windows.com"}
	alibabaNTPHosts = []string{
		"ntp.cloud.aliyuncs.com", "ntp1.cloud.aliyuncs.com", "ntp2.cloud.aliyuncs.com", "ntp3.cloud.aliyuncs.com",
		"ntp4.cloud.aliyuncs.com", "ntp5.cloud.aliyuncs.com", "ntp6.cloud.aliyuncs.com", "ntp7.cloud.aliyuncs.com",
		"ntp8.cloud.aliyuncs.com", "ntp9.cloud.aliyuncs.com", "ntp10.cloud.aliyuncs.com", "ntp11.cloud.aliyuncs.com",
		"ntp12.cloud.aliyuncs.com",
	}
	tencentNTPHosts = []string{"ntpupdate.tencentyun.com"}
	googleNTPHosts  = []string{"0.datadog.pool.ntp.org", "1.datadog.pool.ntp.org", "2.datadog.pool.ntp.org", "3.datadog.pool.ntp.org"}
)

// NTPCheck only has sender and config
type NTPCheck struct {
	core.CheckBase
	cfg            *ntpConfig
	lastCollection time.Time
	errCount       int
}

type ntpInstanceConfig struct {
	OffsetThreshold        int      `yaml:"offset_threshold"`
	Host                   string   `yaml:"host"`
	Hosts                  []string `yaml:"hosts"`
	Port                   int      `yaml:"port"`
	Timeout                int      `yaml:"timeout"`
	Version                int      `yaml:"version"`
	UseLocalDefinedServers bool     `yaml:"use_local_defined_servers"`
}

type ntpInitConfig struct{}

type ntpConfig struct {
	instance ntpInstanceConfig
	initConf ntpInitConfig
}

func (c *NTPCheck) String() string {
	return "ntp"
}

func (c *ntpConfig) parse(data []byte, initData []byte, getLocalServers func() ([]string, error)) error {
	var instance ntpInstanceConfig
	var initConf ntpInitConfig
	defaultVersion := 3
	defaultTimeout := 5
	defaultPort := 123
	defaultOffsetThreshold := 60

	defaultHosts := getCloudProviderNTPHosts()

	if err := yaml.Unmarshal(data, &instance); err != nil {
		return err
	}

	if err := yaml.Unmarshal(initData, &initConf); err != nil {
		return err
	}

	c.instance = instance
	var localNtpServers []string
	var err error
	if c.instance.UseLocalDefinedServers {
		localNtpServers, err = getLocalServers()
		if err != nil {
			return err
		}
		log.Infof("Use local defined servers: %v", localNtpServers)
	}

	if len(localNtpServers) > 0 {
		c.instance.Hosts = localNtpServers
	} else if c.instance.Host != "" {
		hosts := []string{c.instance.Host}
		// If config contains both host and hosts
		for _, h := range c.instance.Hosts {
			if h != c.instance.Host {
				hosts = append(hosts, h)
			}
		}
		c.instance.Hosts = hosts
	}
	if c.instance.Hosts == nil {
		c.instance.Hosts = defaultHosts
	}
	if c.instance.Port == 0 {
		c.instance.Port = defaultPort
	}
	if c.instance.Version == 0 {
		c.instance.Version = defaultVersion
	}
	if c.instance.Timeout == 0 {
		c.instance.Timeout = defaultTimeout
	}
	if c.instance.OffsetThreshold == 0 {
		c.instance.OffsetThreshold = defaultOffsetThreshold
	}
	c.initConf = initConf

	return nil
}

func getCloudProviderNTPHosts() []string {
	if ec2.IsRunningOn() || ecs.IsRunningOn() {
		log.Info("AWS cloud provider detected, using their NTP server.")
		return awsNTPHosts
	} else if gce.IsRunningOn() {
		log.Info("GCP cloud provider detected, using their NTP server.")
		return gcpNTPHosts
	} else if azure.IsRunningOn() {
		log.Info("Azure cloud provider detected, using their NTP server.")
		return azureNTPHosts
	} else if alibaba.IsRunningOn() {
		log.Info("Alibaba cloud provider detected, using their NTP server.")
		return alibabaNTPHosts
	} else if tencent.IsRunningOn() {
		log.Info("Tencent cloud provider detected, using their NTP server.")
		return tencentNTPHosts
	} else {
		log.Info("No cloud provider detected, defaulting to Datadog's NTP server.")
		return googleNTPHosts
	}
}

// Configure configure the data from the yaml
func (c *NTPCheck) Configure(data integration.Data, initConfig integration.Data, source string) error {
	cfg := new(ntpConfig)
	err := cfg.parse(data, initConfig, getLocalDefinedNTPServers)
	if err != nil {
		log.Errorf("Error parsing configuration file: %s", err)
		return err
	}

	c.BuildID(data, initConfig)
	c.cfg = cfg

	err = c.CommonConfigure(data, source)
	if err != nil {
		return err
	}

	return nil
}

// Run runs the check
func (c *NTPCheck) Run() error {
	sender, err := aggregator.GetSender(c.ID())
	if err != nil {
		return err
	}

	var serviceCheckStatus metrics.ServiceCheckStatus
	serviceCheckMessage := ""
	offsetThreshold := c.cfg.instance.OffsetThreshold

	clockOffset, err := c.queryOffset()
	if err != nil {
		log.Info(err)
		serviceCheckStatus = metrics.ServiceCheckUnknown
	} else {
		if int(math.Abs(clockOffset)) > offsetThreshold {
			serviceCheckStatus = metrics.ServiceCheckCritical
			serviceCheckMessage = fmt.Sprintf("Offset %v is higher than offset threshold (%v secs)", clockOffset, offsetThreshold)
		} else {
			serviceCheckStatus = metrics.ServiceCheckOK
		}

		sender.Gauge("ntp.offset", clockOffset, "", nil)
		ntpExpVar.Set(clockOffset)
		tlmNtpOffset.Set(clockOffset)
	}

	sender.ServiceCheck("ntp.in_sync", serviceCheckStatus, "", nil, serviceCheckMessage)

	c.lastCollection = time.Now()

	sender.Commit()

	return nil
}

func (c *NTPCheck) queryOffset() (float64, error) {
	offsets := []float64{}

	for _, host := range c.cfg.instance.Hosts {
		response, err := ntpQuery(host, ntp.QueryOptions{Version: c.cfg.instance.Version, Port: c.cfg.instance.Port, Timeout: time.Duration(c.cfg.instance.Timeout) * time.Second})
		if err != nil {
			if c.errCount >= 10 {
				c.errCount = 0
				log.Warnf("Couldn't query the ntp host %s for 10 times in a row: %s", host, err)
			} else {
				c.errCount++
				log.Debugf("There was an error querying the ntp host %s: %s", host, err)
			}
			continue
		}
		c.errCount = 0
		err = response.Validate()
		if err != nil {
			log.Infof("The ntp response is not valid for host %s: %s", host, err)
			continue
		}
		offsets = append(offsets, response.ClockOffset.Seconds())
	}

	if len(offsets) == 0 {
		return .0, fmt.Errorf("Failed to get clock offset from any ntp host")
	}

	var median float64

	sort.Float64s(offsets)
	length := len(offsets)
	if length%2 == 0 {
		median = (offsets[length/2-1] + offsets[length/2]) / 2.0
	} else {
		median = offsets[length/2]
	}

	return median, nil
}

func ntpFactory() check.Check {
	return &NTPCheck{
		CheckBase: core.NewCheckBaseWithInterval(ntpCheckName, time.Duration(defaultMinCollectionInterval)*time.Second),
	}
}

func init() {
	core.RegisterCheck(ntpCheckName, ntpFactory)
}
