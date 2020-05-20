package main

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1beta1"
)

type configData struct {
	projectID    string
	region       string
	zone         string
	clusterID    string
	targetPoolID string
}

func doUpdate(client *http.Client, config configData) error {
	l := log.WithFields(log.Fields{})
	l.Info("Running update")

	containerService, err := container.New(client)
	if err != nil {
		l.Error(err)
		return err
	}

	computeService, err := compute.New(client)
	if err != nil {
		l.Error(err)
		return err
	}

	l.Trace("Listing NodePools")
	nodePoolsResp, err := containerService.Projects.Zones.Clusters.NodePools.List(config.projectID, config.zone, config.clusterID).Do()
	if err != nil {
		l.Error(err)
		return err
	}

	l.Trace("Getting TargetPool self link")
	targetPoolResp, err := computeService.TargetPools.Get(config.projectID, config.region, config.targetPoolID).Do()
	if err != nil {
		l.Error(err)
		return err
	}
	targetPoolURL := targetPoolResp.SelfLink

	for _, nodePool := range nodePoolsResp.NodePools {
		l := l.WithFields(log.Fields{
			"nodePool": nodePool.Name,
		})
		for _, instanceGroupURL := range nodePool.InstanceGroupUrls {
			instanceGroupName := instanceGroupURL[strings.LastIndex(instanceGroupURL, "/")+1:]
			l := l.WithFields(log.Fields{
				"instanceGroup": instanceGroupName,
			})
			l.Infof("Processing InstanceGroup %s", instanceGroupName)

			l.Trace("Read InstanceGroupManager")
			instanceGroupMgrResp, err := computeService.InstanceGroupManagers.Get(config.projectID, config.zone, instanceGroupName).Do()
			if err != nil {
				l.Error(err)
				return err
			}
			existingTargetPools := instanceGroupMgrResp.TargetPools

			hasPool := false
			for _, existingTargetPool := range existingTargetPools {
				if existingTargetPool == targetPoolURL {
					hasPool = true
					break
				}
			}
			if hasPool {
				l.Debug("InstanceGroupManager already has TargetPool")
				continue
			}

			newTargetPools := append(existingTargetPools, targetPoolURL)
			l.Debugf("New pools: %s", newTargetPools)

			l.Info("Updating InstanceGroupManager TargetPools")
			l.Trace("SetTargetPools")
			setTargetPoolsResp, err := computeService.InstanceGroupManagers.SetTargetPools(config.projectID, config.zone, instanceGroupName, &compute.InstanceGroupManagersSetTargetPoolsRequest{
				TargetPools: newTargetPools,
			}).Do()
			if err != nil {
				l.Error(err)
				return err
			}

			if setTargetPoolsResp.Error != nil {
				l.Error(setTargetPoolsResp.Error.Errors)
			}
			if len(setTargetPoolsResp.Warnings) > 0 {
				l.Warn(setTargetPoolsResp.Warnings)
			}
		}
	}

	return nil
}

func run(ctx context.Context, config configData) error {
	client, err := google.DefaultClient(ctx, compute.ComputeScope, container.CloudPlatformScope)
	if err != nil {
		return err
	}

	for {
		doUpdate(client, config)
		time.Sleep(1 * time.Minute)
	}
}

func main() {
	log.SetLevel(log.TraceLevel)

	config := configData{}
	flag.StringVar(&config.projectID, "project", "", "Project name")
	flag.StringVar(&config.region, "region", "", "Region")
	flag.StringVar(&config.zone, "zone", "", "Zone")
	flag.StringVar(&config.clusterID, "cluster", "", "Cluster name")
	flag.StringVar(&config.targetPoolID, "targetPool", "", "Target pool name")
	flag.Parse()

	if config.projectID == "" {
		log.Fatal("Project name required")
	}
	if config.zone == "" {
		log.Fatal("Zone required")
	}
	if config.region == "" {
		config.region = config.zone[:strings.LastIndex(config.zone, "-")]
		log.Warnf("Inferring region %s", config.region)
	}
	if config.clusterID == "" {
		log.Fatal("Cluster name required")
	}
	if config.targetPoolID == "" {
		log.Fatal("Target pool name required")
	}

	ctx := context.Background()

	if err := run(ctx, config); err != nil {
		log.Error(err)
	}
}
