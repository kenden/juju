// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cachetest

import (
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/core/cache"
	"github.com/juju/juju/core/life"
	"github.com/juju/juju/core/lxdprofile"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/state"
)

// ModelChangeFromState returns a ModelChange representing the current
// model for the state object.
func ModelChangeFromState(c *gc.C, st *state.State) cache.ModelChange {
	m, err := st.Model()
	c.Assert(err, jc.ErrorIsNil)
	return ModelChange(c, m)
}

// ModelChange returns a ModelChange representing the input state model.
func ModelChange(c *gc.C, model *state.Model) cache.ModelChange {
	cfg, err := model.Config()
	c.Assert(err, jc.ErrorIsNil)

	status, err := model.Status()
	c.Assert(err, jc.ErrorIsNil)

	return cache.ModelChange{
		ModelUUID: model.UUID(),
		Name:      model.Name(),
		Life:      life.Value(model.Life().String()),
		Owner:     model.Owner().Name(),
		Config:    cfg.AllAttrs(),
		Status:    status,
	}
}

// CharmChange returns a CharmChange representing the input state charm.
func CharmChange(modelUUID string, ch *state.Charm) cache.CharmChange {
	prof := ch.LXDProfile()
	cProf := lxdprofile.Profile{
		Config:      prof.Config,
		Description: prof.Description,
		Devices:     prof.Devices,
	}

	return cache.CharmChange{
		ModelUUID:     modelUUID,
		CharmURL:      ch.URL().String(),
		CharmVersion:  ch.Version(),
		LXDProfile:    cProf,
		DefaultConfig: ch.Config().DefaultSettings(),
	}
}

// ApplicationChange returns an ApplicationChange
// representing the input state application.
func ApplicationChange(c *gc.C, modelUUID string, app *state.Application) cache.ApplicationChange {
	// Note that this will include charm defaults as if explicitly set.
	// If this matters for tests, we will have to pass a state and attempt
	// to access the settings document for this application charm config.
	config, err := app.CharmConfig(model.GenerationMaster)
	c.Assert(err, jc.ErrorIsNil)

	cons, err := app.Constraints()
	c.Assert(err, jc.ErrorIsNil)

	sts, err := app.Status()
	c.Assert(err, jc.ErrorIsNil)

	cURL, _ := app.CharmURL()

	return cache.ApplicationChange{
		ModelUUID:   modelUUID,
		Name:        app.Name(),
		Exposed:     app.IsExposed(),
		CharmURL:    cURL.Path(),
		Life:        life.Value(app.Life().String()),
		MinUnits:    app.MinUnits(),
		Constraints: cons,
		Config:      config,
		Status:      sts,
		// TODO: Subordinate, WorkloadVersion.
	}
}

func MachineChange(c *gc.C, modelUUID string, machine *state.Machine) cache.MachineChange {
	iid, err := machine.InstanceId()
	c.Assert(err, jc.ErrorIsNil)

	aSts, err := machine.Status()
	c.Assert(err, jc.ErrorIsNil)

	iSts, err := machine.InstanceStatus()
	c.Assert(err, jc.ErrorIsNil)

	hwc, err := machine.HardwareCharacteristics()
	c.Assert(err, jc.ErrorIsNil)

	chProf, err := machine.CharmProfiles()
	c.Assert(err, jc.ErrorIsNil)

	sc, scKnown := machine.SupportedContainers()

	return cache.MachineChange{
		ModelUUID:                modelUUID,
		Id:                       machine.Id(),
		InstanceId:               string(iid),
		AgentStatus:              aSts,
		InstanceStatus:           iSts,
		Life:                     life.Value(machine.Life().String()),
		Series:                   machine.Series(),
		ContainerType:            string(machine.ContainerType()),
		SupportedContainers:      sc,
		SupportedContainersKnown: scKnown,
		HardwareCharacteristics:  hwc,
		CharmProfiles:            chProf,
		HasVote:                  machine.HasVote(),
		// TODO: Config, Addresses.
	}

}

// UnitChange returns a UnitChange representing the input state unit.
func UnitChange(c *gc.C, modelUUID string, unit *state.Unit) cache.UnitChange {
	publicAddr, err := unit.PublicAddress()
	c.Assert(err, jc.ErrorIsNil)

	privateAddr, err := unit.PrivateAddress()
	c.Assert(err, jc.ErrorIsNil)

	machineId, err := unit.AssignedMachineId()
	c.Assert(err, jc.ErrorIsNil)

	pr, err := unit.OpenedPorts()
	c.Assert(err, jc.ErrorIsNil)

	sts, err := unit.Status()
	c.Assert(err, jc.ErrorIsNil)

	aSts, err := unit.AgentStatus()
	c.Assert(err, jc.ErrorIsNil)

	principal, _ := unit.PrincipalName()
	cURL, _ := unit.CharmURL()

	return cache.UnitChange{
		ModelUUID:      modelUUID,
		Name:           unit.Name(),
		Application:    unit.ApplicationName(),
		Series:         unit.Series(),
		CharmURL:       cURL.Path(),
		Life:           life.Value(unit.Life().String()),
		PublicAddress:  publicAddr.String(),
		PrivateAddress: privateAddr.String(),
		MachineId:      machineId,
		PortRanges:     pr,
		Principal:      principal,
		WorkloadStatus: sts,
		AgentStatus:    aSts,
		// TODO: Subordinate
	}
}
