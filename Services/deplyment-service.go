package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sathwikshetty33/Django-vpc/Providers"
	"strings"
	"time"
)

type DeploymentService struct{}

type DeploymentRequest struct {
	RepoURL            string            `json:"repo_url"`
	GithubToken        string            `json:"github_token"`
	Username           string            `json:"username"`
	AdditionalCommands []string          `json:"additional_commands"`
	EnvVariables       map[string]string `json:"env_variables"`
	ASGI               bool              `json:"asgi"`
	AutoDeploy         bool              `json:"auto_deploy"`
}

func NewDeploymentService() *DeploymentService {
	return &DeploymentService{}
}
func (ds *DeploymentService) Deploy(req *DeploymentRequest) (string, error) {
	repoName, err := extractRepoName(req.RepoURL)
	if err != nil {
		return "", fmt.Errorf("failed to extract repo name: %v", err)
	}

	basePath := filepath.Join("deployments", req.Username, repoName)
	
	timestamp := time.Now().Format("20060102-150405")
	workDir := filepath.Join(basePath, timestamp)
	
	log.Printf("Creating deployment directory: %s", workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create work directory: %v", err)
	}

	defer func() {
		log.Printf("Cleaning up deployment directory: %s", workDir)
		if err := os.RemoveAll(workDir); err != nil {
			log.Printf("Warning: failed to cleanup directory %s: %v", workDir, err)
		}
	}()

	terraformDir := filepath.Join(workDir, "terraform")
	if err := os.MkdirAll(terraformDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create terraform directory: %v", err)
	}

	ansibleDir := filepath.Join(workDir, "ansible")
	if err := os.MkdirAll(ansibleDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create ansible directory: %v", err)
	}

	azure := providers.AzureProvider{
		ResourceGroup: fmt.Sprintf("%s-%s-rg", req.Username, repoName),
		Location:      "East US",
		VMSize:        "Standard_B4ms",
		VMName:        fmt.Sprintf("%s-%s-vm", req.Username, repoName),
	}

	log.Println("Generating SSH keys...")
	_, _, err = azure.GenerateSSHKeys(terraformDir)
	if err != nil {
		return "", fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	log.Println("Generating Terraform configuration...")
	if err := azure.GenerateTerraformConfig(terraformDir); err != nil {
		return "", fmt.Errorf("failed to generate terraform config: %v", err)
	}

	log.Println("Initializing Terraform...")
	if err := azure.InitTerraform(terraformDir); err != nil {
		return "", fmt.Errorf("failed to initialize terraform: %v", err)
	}

	log.Println("Applying Terraform...")
	if err := azure.ApplyTerraform(terraformDir); err != nil {
		return "", fmt.Errorf("failed to apply terraform: %v", err)
	}

	log.Println("Retrieving public IP...")
	publicIP, err := azure.GetTerraformOutput(terraformDir, "public_ip")
	if err != nil {
		return "", fmt.Errorf("failed to get public IP: %v", err)
	}

	publicIP = strings.TrimSpace(publicIP)
	if publicIP == "" {
		return "", fmt.Errorf("public IP is empty")
	}

	log.Printf("Retrieved public IP: %s", publicIP)

	log.Println("Using generated SSH keys for Ansible...")
	azurePrivateKeyPath := filepath.Join(terraformDir, "azure_vm_key")
	azurePublicKeyPath := filepath.Join(terraformDir, "azure_vm_key.pub")
	
	if _, err := os.Stat(azurePrivateKeyPath); err != nil {
		return "", fmt.Errorf("Private key not found at %s: %v", azurePrivateKeyPath, err)
	}
	if _, err := os.Stat(azurePublicKeyPath); err != nil {
		return "", fmt.Errorf("Public key not found at %s: %v", azurePublicKeyPath, err)
	}

	log.Printf("Using private key: %s", azurePrivateKeyPath)
	log.Printf("Using public key: %s", azurePublicKeyPath)

	if err := ds.createAnsibleFiles(ansibleDir, req, publicIP, azurePrivateKeyPath); err != nil {
		return "", fmt.Errorf("failed to create ansible files: %v", err)
	}

	log.Println("Waiting for VM to be ready...")
	time.Sleep(60 * time.Second)

	log.Println("Testing SSH connectivity...")
	if err := ds.testSSHConnectivity(publicIP, azurePrivateKeyPath); err != nil {
		log.Printf("SSH connectivity test failed, but continuing: %v", err)
		time.Sleep(30 * time.Second)
	}

	log.Println("Running Ansible playbook...")
	if err := ds.runAnsiblePlaybook(ansibleDir); err != nil {
		return "", fmt.Errorf("failed to run ansible playbook: %v", err)
	}

	if req.AutoDeploy {
		log.Println("Setting up GitHub Actions auto-deployment using existing SSH keys...")
		if err := ds.setupGitHubActionsOnServer(ansibleDir, req, publicIP, terraformDir); err != nil {
			log.Printf("Warning: failed to setup GitHub Actions: %v", err)
		}

		if err := ds.runAdditionalAnsibleTasks(ansibleDir); err != nil {
			log.Printf("Warning: failed to run GitHub Actions setup tasks: %v", err)
		}
	}

	log.Println("Deployment completed successfully!")
	return publicIP, nil
}


func (ds *DeploymentService) testSSHConnectivity(publicIP, privateKeyPath string) error {
	cmd := exec.Command("ssh", 
		"-i", privateKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("azureuser@%s", publicIP),
		"echo 'SSH test successful'")
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SSH test failed: %v, output: %s", err, string(output))
	}
	
	log.Printf("SSH test output: %s", string(output))
	return nil
}