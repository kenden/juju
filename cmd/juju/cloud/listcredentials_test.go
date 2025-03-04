// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud_test

import (
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/cmd/cmdtesting"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/juju/cloud"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	"github.com/juju/juju/testing"
)

type listCredentialsSuite struct {
	testing.BaseSuite
	store              *jujuclient.MemStore
	personalCloudsFunc func() (map[string]jujucloud.Cloud, error)
	cloudByNameFunc    func(string) (*jujucloud.Cloud, error)
}

var _ = gc.Suite(&listCredentialsSuite{
	personalCloudsFunc: func() (map[string]jujucloud.Cloud, error) {
		return map[string]jujucloud.Cloud{
			"mycloud":      {},
			"missingcloud": {},
		}, nil
	},
	cloudByNameFunc: func(name string) (*jujucloud.Cloud, error) {
		if name == "missingcloud" {
			return nil, errors.NotValidf(name)
		}
		return &jujucloud.Cloud{Type: "test-provider"}, nil
	},
})

func (s *listCredentialsSuite) SetUpSuite(c *gc.C) {
	s.BaseSuite.SetUpSuite(c)
	unreg := environs.RegisterProvider("test-provider", &mockProvider{})
	s.AddCleanup(func(_ *gc.C) {
		unreg()
	})
}

func (s *listCredentialsSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.store = &jujuclient.MemStore{
		Credentials: map[string]jujucloud.CloudCredential{
			"aws": {
				DefaultRegion:     "ap-southeast-2",
				DefaultCredential: "down",
				AuthCredentials: map[string]jujucloud.Credential{
					"bob": jujucloud.NewCredential(
						jujucloud.AccessKeyAuthType,
						map[string]string{
							"access-key": "key",
							"secret-key": "secret",
						},
					),
					"down": jujucloud.NewCredential(
						jujucloud.UserPassAuthType,
						map[string]string{
							"username": "user",
							"password": "password",
						},
					),
				},
			},
			"google": {
				AuthCredentials: map[string]jujucloud.Credential{
					"default": jujucloud.NewCredential(
						jujucloud.OAuth2AuthType,
						map[string]string{
							"client-id":    "id",
							"client-email": "email",
							"private-key":  "key",
						},
					),
				},
			},
			"azure": {
				AuthCredentials: map[string]jujucloud.Credential{
					"azhja": jujucloud.NewCredential(
						jujucloud.UserPassAuthType,
						map[string]string{
							"application-id":       "app-id",
							"application-password": "app-secret",
							"subscription-id":      "subscription-id",
							"tenant-id":            "tenant-id",
						},
					),
				},
			},
			"mycloud": {
				AuthCredentials: map[string]jujucloud.Credential{
					"me": jujucloud.NewCredential(
						jujucloud.AccessKeyAuthType,
						map[string]string{
							"access-key": "key",
							"secret-key": "secret",
						},
					),
				},
			},
		},
	}
}

func (s *listCredentialsSuite) TestListCredentialsTabular(c *gc.C) {
	out := s.listCredentials(c)
	c.Assert(out, gc.Equals, `
Cloud    Credentials
aws      down*, bob
azure    azhja
google   default
mycloud  me

`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsTabularInvalidCredential(c *gc.C) {
	store := jujuclienttesting.WrapClientStore(s.store)
	store.CredentialForCloudFunc = func(cloudName string) (*jujucloud.CloudCredential, error) {
		if cloudName == "mycloud" {
			return nil, errors.Errorf("expected error")
		}
		return s.store.CredentialForCloud(cloudName)
	}

	var logWriter loggo.TestWriter
	writerName := "TestListCredentialsTabularInvalidCredential"
	c.Assert(loggo.RegisterWriter(writerName, &logWriter), jc.ErrorIsNil)
	defer func() {
		loggo.RemoveWriter(writerName)
		logWriter.Clear()
	}()

	ctx := s.listCredentialsWithStore(c, store)
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
Cloud   Credentials
aws     down*, bob
azure   azhja
google  default

`[1:])
	c.Check(logWriter.Log(), jc.LogMatches, []jc.SimpleMessage{
		{
			Level:   loggo.WARNING,
			Message: `error loading credential for cloud mycloud: expected error`,
		},
	})
}

func (s *listCredentialsSuite) TestListCredentialsTabularMissingCloud(c *gc.C) {
	s.store.Credentials["missingcloud"] = jujucloud.CloudCredential{}
	out := s.listCredentials(c)
	c.Assert(out, gc.Equals, `
The following clouds have been removed and are omitted from the results to avoid leaking secrets.
Run with --show-secrets to display these clouds' credentials: missingcloud

Cloud    Credentials
aws      down*, bob
azure    azhja
google   default
mycloud  me

`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsTabularFiltered(c *gc.C) {
	out := s.listCredentials(c, "aws")
	c.Assert(out, gc.Equals, `
Cloud  Credentials
aws    down*, bob

`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsYAMLWithSecrets(c *gc.C) {
	s.store.Credentials["missingcloud"] = jujucloud.CloudCredential{
		AuthCredentials: map[string]jujucloud.Credential{
			"default": jujucloud.NewCredential(
				jujucloud.AccessKeyAuthType,
				map[string]string{
					"access-key": "key",
					"secret-key": "secret",
				},
			),
		},
	}
	out := s.listCredentials(c, "--format", "yaml", "--show-secrets")
	c.Assert(out, gc.Equals, `
local-credentials:
  aws:
    default-credential: down
    default-region: ap-southeast-2
    bob:
      auth-type: access-key
      access-key: key
      secret-key: secret
    down:
      auth-type: userpass
      password: password
      username: user
  azure:
    azhja:
      auth-type: userpass
      application-id: app-id
      application-password: app-secret
      subscription-id: subscription-id
      tenant-id: tenant-id
  google:
    default:
      auth-type: oauth2
      client-email: email
      client-id: id
      private-key: key
  missingcloud:
    default:
      auth-type: access-key
      access-key: key
      secret-key: secret
  mycloud:
    me:
      auth-type: access-key
      access-key: key
      secret-key: secret
`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsYAMLWithSecretsInvalidCredential(c *gc.C) {
	s.store.Credentials["missingcloud"] = jujucloud.CloudCredential{
		AuthCredentials: map[string]jujucloud.Credential{
			"default": jujucloud.NewCredential(
				jujucloud.AccessKeyAuthType,
				map[string]string{
					"access-key": "key",
					"secret-key": "secret",
				},
			),
		},
	}
	store := jujuclienttesting.WrapClientStore(s.store)
	store.CredentialForCloudFunc = func(cloudName string) (*jujucloud.CloudCredential, error) {
		if cloudName == "mycloud" {
			return nil, errors.Errorf("expected error")
		}
		return s.store.CredentialForCloud(cloudName)
	}

	var logWriter loggo.TestWriter
	writerName := "TestListCredentialsYAMLWithSecretsInvalidCredential"
	c.Assert(loggo.RegisterWriter(writerName, &logWriter), jc.ErrorIsNil)
	defer func() {
		loggo.RemoveWriter(writerName)
		logWriter.Clear()
	}()

	ctx := s.listCredentialsWithStore(c, store, "--format", "yaml", "--show-secrets")
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
local-credentials:
  aws:
    default-credential: down
    default-region: ap-southeast-2
    bob:
      auth-type: access-key
      access-key: key
      secret-key: secret
    down:
      auth-type: userpass
      password: password
      username: user
  azure:
    azhja:
      auth-type: userpass
      application-id: app-id
      application-password: app-secret
      subscription-id: subscription-id
      tenant-id: tenant-id
  google:
    default:
      auth-type: oauth2
      client-email: email
      client-id: id
      private-key: key
  missingcloud:
    default:
      auth-type: access-key
      access-key: key
      secret-key: secret
`[1:])
	c.Check(logWriter.Log(), jc.LogMatches, []jc.SimpleMessage{
		{
			Level:   loggo.WARNING,
			Message: `error loading credential for cloud mycloud: expected error`,
		},
	})
}

func (s *listCredentialsSuite) TestListCredentialsYAMLNoSecrets(c *gc.C) {
	s.store.Credentials["missingcloud"] = jujucloud.CloudCredential{
		AuthCredentials: map[string]jujucloud.Credential{
			"default": jujucloud.NewCredential(
				jujucloud.AccessKeyAuthType,
				map[string]string{
					"access-key": "key",
					"secret-key": "secret",
				},
			),
		},
	}
	out := s.listCredentials(c, "--format", "yaml")
	c.Assert(out, gc.Equals, `
local-credentials:
  aws:
    default-credential: down
    default-region: ap-southeast-2
    bob:
      auth-type: access-key
      access-key: key
    down:
      auth-type: userpass
      username: user
  azure:
    azhja:
      auth-type: userpass
      application-id: app-id
      subscription-id: subscription-id
      tenant-id: tenant-id
  google:
    default:
      auth-type: oauth2
      client-email: email
      client-id: id
  mycloud:
    me:
      auth-type: access-key
      access-key: key
`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsYAMLFiltered(c *gc.C) {
	out := s.listCredentials(c, "--format", "yaml", "azure")
	c.Assert(out, gc.Equals, `
local-credentials:
  azure:
    azhja:
      auth-type: userpass
      application-id: app-id
      subscription-id: subscription-id
      tenant-id: tenant-id
`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsJSONWithSecrets(c *gc.C) {
	out := s.listCredentials(c, "--format", "json", "--show-secrets")
	c.Assert(out, gc.Equals, `
{"local-credentials":{"aws":{"default-credential":"down","default-region":"ap-southeast-2","cloud-credentials":{"bob":{"auth-type":"access-key","details":{"access-key":"key","secret-key":"secret"}},"down":{"auth-type":"userpass","details":{"password":"password","username":"user"}}}},"azure":{"cloud-credentials":{"azhja":{"auth-type":"userpass","details":{"application-id":"app-id","application-password":"app-secret","subscription-id":"subscription-id","tenant-id":"tenant-id"}}}},"google":{"cloud-credentials":{"default":{"auth-type":"oauth2","details":{"client-email":"email","client-id":"id","private-key":"key"}}}},"mycloud":{"cloud-credentials":{"me":{"auth-type":"access-key","details":{"access-key":"key","secret-key":"secret"}}}}}}
`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsJSONWithSecretsInvalidCredential(c *gc.C) {
	store := jujuclienttesting.WrapClientStore(s.store)
	store.CredentialForCloudFunc = func(cloudName string) (*jujucloud.CloudCredential, error) {
		if cloudName == "mycloud" {
			return nil, errors.Errorf("expected error")
		}
		return s.store.CredentialForCloud(cloudName)
	}

	var logWriter loggo.TestWriter
	writerName := "TestListCredentialsJSONWithSecretsInvalidCredential"
	c.Assert(loggo.RegisterWriter(writerName, &logWriter), jc.ErrorIsNil)
	defer func() {
		loggo.RemoveWriter(writerName)
		logWriter.Clear()
	}()

	ctx := s.listCredentialsWithStore(c, store, "--format", "json", "--show-secrets")
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
{"local-credentials":{"aws":{"default-credential":"down","default-region":"ap-southeast-2","cloud-credentials":{"bob":{"auth-type":"access-key","details":{"access-key":"key","secret-key":"secret"}},"down":{"auth-type":"userpass","details":{"password":"password","username":"user"}}}},"azure":{"cloud-credentials":{"azhja":{"auth-type":"userpass","details":{"application-id":"app-id","application-password":"app-secret","subscription-id":"subscription-id","tenant-id":"tenant-id"}}}},"google":{"cloud-credentials":{"default":{"auth-type":"oauth2","details":{"client-email":"email","client-id":"id","private-key":"key"}}}}}}
`[1:])
	c.Check(logWriter.Log(), jc.LogMatches, []jc.SimpleMessage{
		{
			Level:   loggo.WARNING,
			Message: `error loading credential for cloud mycloud: expected error`,
		},
	})
}

func (s *listCredentialsSuite) TestListCredentialsJSONNoSecrets(c *gc.C) {
	out := s.listCredentials(c, "--format", "json")
	c.Assert(out, gc.Equals, `
{"local-credentials":{"aws":{"default-credential":"down","default-region":"ap-southeast-2","cloud-credentials":{"bob":{"auth-type":"access-key","details":{"access-key":"key"}},"down":{"auth-type":"userpass","details":{"username":"user"}}}},"azure":{"cloud-credentials":{"azhja":{"auth-type":"userpass","details":{"application-id":"app-id","subscription-id":"subscription-id","tenant-id":"tenant-id"}}}},"google":{"cloud-credentials":{"default":{"auth-type":"oauth2","details":{"client-email":"email","client-id":"id"}}}},"mycloud":{"cloud-credentials":{"me":{"auth-type":"access-key","details":{"access-key":"key"}}}}}}
`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsJSONFiltered(c *gc.C) {
	out := s.listCredentials(c, "--format", "json", "azure")
	c.Assert(out, gc.Equals, `
{"local-credentials":{"azure":{"cloud-credentials":{"azhja":{"auth-type":"userpass","details":{"application-id":"app-id","subscription-id":"subscription-id","tenant-id":"tenant-id"}}}}}}
`[1:])
}

func (s *listCredentialsSuite) TestListCredentialsEmpty(c *gc.C) {
	s.store = &jujuclient.MemStore{
		Credentials: map[string]jujucloud.CloudCredential{
			"aws": {
				AuthCredentials: map[string]jujucloud.Credential{
					"bob": jujucloud.NewCredential(
						jujucloud.OAuth2AuthType,
						map[string]string{},
					),
				},
			},
		},
	}
	out := strings.Replace(s.listCredentials(c), "\n", "", -1)
	c.Assert(out, gc.Equals, "Cloud  Credentialsaws    bob")

	out = strings.Replace(s.listCredentials(c, "--format", "yaml"), "\n", "", -1)
	c.Assert(out, gc.Equals, "local-credentials:  aws:    bob:      auth-type: oauth2")

	out = strings.Replace(s.listCredentials(c, "--format", "json"), "\n", "", -1)
	c.Assert(out, gc.Equals, `{"local-credentials":{"aws":{"cloud-credentials":{"bob":{"auth-type":"oauth2"}}}}}`)
}

func (s *listCredentialsSuite) TestListCredentialsNone(c *gc.C) {
	listCmd := cloud.NewListCredentialsCommandForTest(jujuclient.NewMemStore(), s.personalCloudsFunc, s.cloudByNameFunc)
	ctx, err := cmdtesting.RunCommand(c, listCmd)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
	out := strings.Replace(cmdtesting.Stdout(ctx), "\n", "", -1)
	c.Assert(out, gc.Equals, "No locally stored credentials to display.")

	ctx, err = cmdtesting.RunCommand(c, listCmd, "--format", "yaml")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
	out = strings.Replace(cmdtesting.Stdout(ctx), "\n", "", -1)
	c.Assert(out, gc.Equals, "local-credentials: {}")

	ctx, err = cmdtesting.RunCommand(c, listCmd, "--format", "json")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
	out = strings.Replace(cmdtesting.Stdout(ctx), "\n", "", -1)
	c.Assert(out, gc.Equals, `{"local-credentials":{}}`)
}

func (s *listCredentialsSuite) listCredentials(c *gc.C, args ...string) string {
	ctx := s.listCredentialsWithStore(c, s.store, args...)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "")
	return cmdtesting.Stdout(ctx)
}

func (s *listCredentialsSuite) listCredentialsWithStore(c *gc.C, store jujuclient.ClientStore, args ...string) *cmd.Context {
	ctx, err := cmdtesting.RunCommand(c, cloud.NewListCredentialsCommandForTest(store, s.personalCloudsFunc, s.cloudByNameFunc), args...)
	c.Assert(err, jc.ErrorIsNil)
	return ctx
}
