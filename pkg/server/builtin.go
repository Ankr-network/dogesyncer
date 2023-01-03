package server

import (
	"github.com/ankr/dogesyncer/secrets"
	"github.com/ankr/dogesyncer/secrets/awsssm"
	"github.com/ankr/dogesyncer/secrets/hashicorpvault"
	"github.com/ankr/dogesyncer/secrets/local"
)

// secretsManagerBackends defines the SecretManager factories for different
// secret management solutions
var secretsManagerBackends = map[secrets.SecretsManagerType]secrets.SecretsManagerFactory{
	secrets.Local:          local.SecretsManagerFactory,
	secrets.HashicorpVault: hashicorpvault.SecretsManagerFactory,
	secrets.AWSSSM:         awsssm.SecretsManagerFactory,
}
