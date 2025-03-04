// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/gnuflag"

	jujucloud "github.com/juju/juju/cloud"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/output"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/jujuclient"
)

var usageListCredentialsSummary = `
Lists locally stored credentials for a cloud.`[1:]

var usageListCredentialsDetails = `
Locally stored credentials are used with `[1:] + "`juju bootstrap`" + `  
and ` + "`juju add-model`" + `.

An arbitrary "credential name" is used to represent credentials, which are 
added either via ` + "`juju add-credential` or `juju autoload-credentials`" + `.
Note that there can be multiple sets of credentials and, thus, multiple 
names.

Actual authentication material is exposed with the '--show-secrets' 
option.

A controller, and subsequently created models, can be created with a 
different set of credentials but any action taken within the model (e.g.:
` + "`juju deploy`; `juju add-unit`" + `) applies the credential used 
to create that model. This model credential is stored on the controller. 

A credential for 'controller' model is determined at bootstrap time and
will be stored on the controller. It is considered to be controller default.

Recall that when a controller is created a 'default' model is also 
created. This model will use the controller default credential. To see all your
credentials on the controller use "juju show-credentials" command.

When adding a new model, Juju will reuse the controller default credential.
To add a model that uses a different credential, specify a locally
stored credential using --credential option. See ` + "`juju help add-model`" + ` 
for more information.

Credentials denoted with an asterisk '*' are currently set as the local default
for the given cloud.

Examples:
    juju credentials
    juju credentials aws
    juju credentials --format yaml --show-secrets

See also: 
    add-credential
    remove-credential
    set-default-credential
    autoload-credentials
    show-credentials
`

type listCredentialsCommand struct {
	cmd.CommandBase
	out         cmd.Output
	cloudName   string
	showSecrets bool

	store              jujuclient.CredentialGetter
	personalCloudsFunc func() (map[string]jujucloud.Cloud, error)
	cloudByNameFunc    func(string) (*jujucloud.Cloud, error)
}

// CloudCredential contains attributes used to define credentials for a cloud.
type CloudCredential struct {
	// DefaultCredential is the named credential to use by default.
	DefaultCredential string `json:"default-credential,omitempty" yaml:"default-credential,omitempty"`

	// DefaultRegion is the cloud region to use by default.
	DefaultRegion string `json:"default-region,omitempty" yaml:"default-region,omitempty"`

	// Credentials is the collection of all credentials registered by the user for a cloud, keyed on a cloud name.
	Credentials map[string]Credential `json:"cloud-credentials,omitempty" yaml:",omitempty,inline"`
}

// Credential instances represent cloud credentials.
type Credential struct {
	// AuthType determines authentication type for the credential.
	AuthType string `json:"auth-type" yaml:"auth-type"`

	// Attributes define details for individual credential.
	// This collection is provider-specific: each provider is interested in different credential details.
	Attributes map[string]string `json:"details,omitempty" yaml:",omitempty,inline"`

	// Revoked is true if the credential has been revoked.
	Revoked bool `json:"revoked,omitempty" yaml:"revoked,omitempty"`

	// Label is optionally set to describe the credentials to a user.
	Label string `json:"label,omitempty" yaml:"label,omitempty"`
}

type credentialsMap struct {
	Credentials map[string]CloudCredential `yaml:"local-credentials" json:"local-credentials"`
}

// NewListCredentialsCommand returns a command to list cloud credentials.
func NewListCredentialsCommand() cmd.Command {
	return &listCredentialsCommand{
		store:           jujuclient.NewFileCredentialStore(),
		cloudByNameFunc: jujucloud.CloudByName,
	}
}

func (c *listCredentialsCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "credentials",
		Args:    "[<cloud name>]",
		Purpose: usageListCredentialsSummary,
		Doc:     usageListCredentialsDetails,
		Aliases: []string{"list-credentials"},
	})
}

func (c *listCredentialsCommand) SetFlags(f *gnuflag.FlagSet) {
	c.CommandBase.SetFlags(f)
	f.BoolVar(&c.showSecrets, "show-secrets", false, "Show secrets")
	c.out.AddFlags(f, "tabular", map[string]cmd.Formatter{
		"yaml":    cmd.FormatYaml,
		"json":    cmd.FormatJson,
		"tabular": formatCredentialsTabular,
	})
}

func (c *listCredentialsCommand) Init(args []string) error {
	cloudName, err := cmd.ZeroOrOneArgs(args)
	if err != nil {
		return errors.Trace(err)
	}
	c.cloudName = cloudName
	return nil
}

func (c *listCredentialsCommand) personalClouds() (map[string]jujucloud.Cloud, error) {
	if c.personalCloudsFunc == nil {
		return jujucloud.PersonalCloudMetadata()
	}
	return c.personalCloudsFunc()
}

func (c *listCredentialsCommand) cloudNames() ([]string, error) {
	if c.cloudName != "" {
		return []string{c.cloudName}, nil
	}
	personalClouds, err := c.personalClouds()
	if err != nil {
		return nil, err
	}
	publicClouds, _, err := jujucloud.PublicCloudMetadata(jujucloud.JujuPublicCloudsPath())
	if err != nil {
		return nil, errors.Trace(err)
	}
	builtinClouds, err := common.BuiltInClouds()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return c.sortClouds(personalClouds, publicClouds, builtinClouds), nil
}

func (c *listCredentialsCommand) sortClouds(maps ...map[string]jujucloud.Cloud) []string {
	var clouds []string
	for _, m := range maps {
		for name := range m {
			clouds = append(clouds, name)
		}
	}
	sort.Strings(clouds)
	return clouds
}

func (c *listCredentialsCommand) Run(ctxt *cmd.Context) error {
	cloudNames, err := c.cloudNames()
	if err != nil {
		return errors.Annotatef(err, "failed to list available clouds")
	}

	displayCredentials := make(map[string]CloudCredential)
	var missingClouds []string
	for _, cloudName := range cloudNames {
		cred, err := c.store.CredentialForCloud(cloudName)
		if errors.IsNotFound(err) {
			continue
		} else if err != nil {
			ctxt.Warningf("error loading credential for cloud %v: %v", cloudName, err)
			continue
		}
		if !c.showSecrets {
			if err := c.removeSecrets(cloudName, cred); err != nil {
				if errors.IsNotValid(err) {
					missingClouds = append(missingClouds, cloudName)
					continue
				}
				return errors.Annotatef(err, "removing secrets from credentials for cloud %v", cloudName)
			}
		}
		displayCredential := CloudCredential{
			DefaultCredential: cred.DefaultCredential,
			DefaultRegion:     cred.DefaultRegion,
		}
		if len(cred.AuthCredentials) != 0 {
			displayCredential.Credentials = make(map[string]Credential, len(cred.AuthCredentials))
			for credName, credDetails := range cred.AuthCredentials {
				displayCredential.Credentials[credName] = Credential{
					string(credDetails.AuthType()),
					credDetails.Attributes(),
					credDetails.Revoked,
					credDetails.Label,
				}
			}
		}
		displayCredentials[cloudName] = displayCredential
	}
	if c.out.Name() == "tabular" && len(missingClouds) > 0 {
		fmt.Fprintf(ctxt.GetStdout(), "The following clouds have been removed and are omitted from the results to avoid leaking secrets.\n"+
			"Run with --show-secrets to display these clouds' credentials: %v\n\n", strings.Join(missingClouds, ", "))
	}
	return c.out.Write(ctxt, credentialsMap{displayCredentials})
}

func (c *listCredentialsCommand) removeSecrets(cloudName string, cloudCred *jujucloud.CloudCredential) error {
	cloud, err := common.CloudOrProvider(cloudName, c.cloudByNameFunc)
	if err != nil {
		return err
	}
	provider, err := environs.Provider(cloud.Type)
	if err != nil {
		return err
	}
	schemas := provider.CredentialSchemas()
	for name, cred := range cloudCred.AuthCredentials {
		sanitisedCred, err := jujucloud.RemoveSecrets(cred, schemas)
		if err != nil {
			return err
		}
		cloudCred.AuthCredentials[name] = *sanitisedCred
	}
	return nil
}

// formatCredentialsTabular writes a tabular summary of cloud information.
func formatCredentialsTabular(writer io.Writer, value interface{}) error {
	credentials, ok := value.(credentialsMap)
	if !ok {
		return errors.Errorf("expected value of type %T, got %T", credentials, value)
	}

	if len(credentials.Credentials) == 0 {
		fmt.Fprintln(writer, "No locally stored credentials to display.")
		return nil
	}

	// For tabular we'll sort alphabetically by cloud, and then by credential name.
	var cloudNames []string
	for name := range credentials.Credentials {
		cloudNames = append(cloudNames, name)
	}
	sort.Strings(cloudNames)

	tw := output.TabWriter(writer)
	w := output.Wrapper{tw}
	w.Println("Cloud", "Credentials")
	for _, cloudName := range cloudNames {
		var haveDefault bool
		var credentialNames []string
		credentials := credentials.Credentials[cloudName]
		for credentialName := range credentials.Credentials {
			if credentialName == credentials.DefaultCredential {
				credentialNames = append([]string{credentialName + "*"}, credentialNames...)
				haveDefault = true
			} else {
				credentialNames = append(credentialNames, credentialName)
			}
		}
		if haveDefault {
			sort.Strings(credentialNames[1:])
		} else {
			sort.Strings(credentialNames)
		}
		w.Println(cloudName, strings.Join(credentialNames, ", "))
	}
	tw.Flush()

	return nil
}
