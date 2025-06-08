package providers

type CloudProvider interface {
	GenerateTerraformConfig(path string) error
	InitTerraform(path string) error
	ApplyTerraform(path string) error
	GenerateSSHKeys(path string) error

}
