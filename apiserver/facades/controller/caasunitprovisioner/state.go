// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasunitprovisioner

import (
	"time"

	"github.com/juju/errors"
	"gopkg.in/juju/charm.v6"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/application"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
)

// CAASUnitProvisionerState provides the subset of global state
// required by the CAAS unit provisioner facade.
type CAASUnitProvisionerState interface {
	ControllerConfig() (controller.Config, error)
	Application(string) (Application, error)
	FindEntity(names.Tag) (state.Entity, error)
	Model() (Model, error)
	WatchApplications() state.StringsWatcher
	ResolveConstraints(cons constraints.Value) (constraints.Value, error)
}

// StorageBackend provides the subset of backend storage
// functionality required by the CAAS unit provisioner facade.
type StorageBackend interface {
	StorageInstance(names.StorageTag) (state.StorageInstance, error)
	Filesystem(names.FilesystemTag) (state.Filesystem, error)
	StorageInstanceFilesystem(names.StorageTag) (state.Filesystem, error)
	UnitStorageAttachments(unit names.UnitTag) ([]state.StorageAttachment, error)
	SetFilesystemInfo(names.FilesystemTag, state.FilesystemInfo) error
	SetFilesystemAttachmentInfo(names.Tag, names.FilesystemTag, state.FilesystemAttachmentInfo) error
	Volume(tag names.VolumeTag) (state.Volume, error)
	StorageInstanceVolume(tag names.StorageTag) (state.Volume, error)
	SetVolumeInfo(names.VolumeTag, state.VolumeInfo) error
	SetVolumeAttachmentInfo(names.Tag, names.VolumeTag, state.VolumeAttachmentInfo) error

	// These are for cleanup up orphaned filesystems when pods are recreated.
	// TODO(caas) - record unit id on the filesystem so we can query by unit
	AllFilesystems() ([]state.Filesystem, error)
	DestroyStorageInstance(tag names.StorageTag, destroyAttachments bool, force bool, maxWait time.Duration) (err error)
	DestroyFilesystem(tag names.FilesystemTag) (err error)
}

// DeviceBackend provides the subset of backend Device
// functionality required by the CAAS unit provisioner facade.
type DeviceBackend interface {
	DeviceConstraints(id string) (map[string]state.DeviceConstraints, error)
}

// Model provides the subset of CAAS model state required
// by the CAAS unit provisioner facade.
type Model interface {
	ModelConfig() (*config.Config, error)
	PodSpec(tag names.ApplicationTag) (string, error)
	WatchPodSpec(tag names.ApplicationTag) (state.NotifyWatcher, error)
	Containers(providerIds ...string) ([]state.CloudContainer, error)
}

// Application provides the subset of application state
// required by the CAAS unit provisioner facade.
type Application interface {
	GetScale() int
	SetScale(int, int64, bool) error
	WatchScale() state.NotifyWatcher
	ApplicationConfig() (application.ConfigAttributes, error)
	AllUnits() (units []Unit, err error)
	AddOperation(state.UnitUpdateProperties) *state.AddUnitOperation
	UpdateUnits(*state.UpdateUnitsOperation) error
	UpdateCloudService(providerId string, addreses []network.Address) error
	StorageConstraints() (map[string]state.StorageConstraints, error)
	DeviceConstraints() (map[string]state.DeviceConstraints, error)
	Life() state.Life
	Name() string
	Tag() names.Tag
	Constraints() (constraints.Value, error)
	GetPlacement() string
	SetOperatorStatus(sInfo status.StatusInfo) error
	SetStatus(statusInfo status.StatusInfo) error
	Charm() (Charm, bool, error)
}

type stateShim struct {
	*state.State
}

func (s stateShim) Application(id string) (Application, error) {
	app, err := s.State.Application(id)
	if err != nil {
		return nil, err
	}
	return applicationShim{app}, nil
}

func (s stateShim) Model() (Model, error) {
	model, err := s.State.Model()
	if err != nil {
		return nil, err
	}
	return model.CAASModel()
}

type applicationShim struct {
	*state.Application
}

func (a applicationShim) AllUnits() ([]Unit, error) {
	all, err := a.Application.AllUnits()
	if err != nil {
		return nil, errors.Trace(err)
	}
	result := make([]Unit, len(all))
	for i, u := range all {
		result[i] = u
	}
	return result, nil
}

func (a applicationShim) Charm() (Charm, bool, error) {
	return a.Application.Charm()
}

type Charm interface {
	Meta() *charm.Meta
}

type Unit interface {
	Name() string
	Life() state.Life
	UnitTag() names.UnitTag
	ContainerInfo() (state.CloudContainer, error)
	AgentStatus() (status.StatusInfo, error)
	UpdateOperation(props state.UnitUpdateProperties) *state.UpdateUnitOperation
	DestroyOperation() *state.DestroyUnitOperation
}
