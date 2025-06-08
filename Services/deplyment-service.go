package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	providers "sathwikshetty33/Django-vpc/Providers"
	"strings"
	"time"
)

type LogBroadcaster interface {
	BroadcastLog(deploymentID string, logMsg LogMessage)
}

type LogMessage struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Step      string `json:"step,omitempty"`
}

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

func (ds *DeploymentService) broadcastLog(broadcaster LogBroadcaster, deploymentID, level, message, step string) {
	if broadcaster != nil {
		broadcaster.BroadcastLog(deploymentID, LogMessage{
			Level:     level,
			Message:   message,
			Timestamp: time.Now().Format(time.RFC3339),
			Step:      step,
		})
	}
}

func (ds *DeploymentService) Deploy(req *DeploymentRequest, deploymentID string, broadcaster LogBroadcaster) (string, error) {
	ds.broadcastLog(broadcaster, deploymentID, "info", "Extracting repository name...", "setup")

	repoName, err := extractRepoName(req.RepoURL)
	if err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to extract repo name: %v", err), "setup")
		return "", fmt.Errorf("failed to extract repo name: %v", err)
	}

	ds.broadcastLog(broadcaster, deploymentID, "info", fmt.Sprintf("Repository name: %s", repoName), "setup")

	basePath := filepath.Join("deployments", req.Username, repoName)
	timestamp := time.Now().Format("20060102-150405")
	workDir := filepath.Join(basePath, timestamp)

	ds.broadcastLog(broadcaster, deploymentID, "info", fmt.Sprintf("Creating deployment directory: %s", workDir), "setup")

	if err := os.MkdirAll(workDir, 0755); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to create work directory: %v", err), "setup")
		return "", fmt.Errorf("failed to create work directory: %v", err)
	}

	defer func() {
		ds.broadcastLog(broadcaster, deploymentID, "info", "Cleaning up deployment directory...", "cleanup")
		if err := os.RemoveAll(workDir); err != nil {
			ds.broadcastLog(broadcaster, deploymentID, "warn", fmt.Sprintf("Failed to cleanup directory %s: %v", workDir, err), "cleanup")
		} else {
			ds.broadcastLog(broadcaster, deploymentID, "info", "Cleanup completed successfully", "cleanup")
		}
	}()

	ds.broadcastLog(broadcaster, deploymentID, "info", "Creating terraform directory...", "setup")
	terraformDir := filepath.Join(workDir, "terraform")
	if err := os.MkdirAll(terraformDir, 0755); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to create terraform directory: %v", err), "setup")
		return "", fmt.Errorf("failed to create terraform directory: %v", err)
	}

	ds.broadcastLog(broadcaster, deploymentID, "info", "Creating ansible directory...", "setup")
	ansibleDir := filepath.Join(workDir, "ansible")
	if err := os.MkdirAll(ansibleDir, 0755); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to create ansible directory: %v", err), "setup")
		return "", fmt.Errorf("failed to create ansible directory: %v", err)
	}

	azure := providers.AzureProvider{
		ResourceGroup: fmt.Sprintf("%s-%s-rg", req.Username, repoName),
		Location:      "East US",
		VMSize:        "Standard_B4ms",
		VMName:        fmt.Sprintf("%s-%s-vm", req.Username, repoName),
	}

	ds.broadcastLog(broadcaster, deploymentID, "info", "Generating SSH keys...", "ssh")
	_, _, err = azure.GenerateSSHKeys(terraformDir)
	if err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to generate SSH keys: %v", err), "ssh")
		return "", fmt.Errorf("failed to generate SSH keys: %v", err)
	}
	ds.broadcastLog(broadcaster, deploymentID, "success", "SSH keys generated successfully", "ssh")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Generating Terraform configuration...", "terraform")
	if err := azure.GenerateTerraformConfig(terraformDir); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to generate terraform config: %v", err), "terraform")
		return "", fmt.Errorf("failed to generate terraform config: %v", err)
	}
	ds.broadcastLog(broadcaster, deploymentID, "success", "Terraform configuration generated", "terraform")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Initializing Terraform...", "terraform")
	if err := azure.InitTerraform(terraformDir); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to initialize terraform: %v", err), "terraform")
		return "", fmt.Errorf("failed to initialize terraform: %v", err)
	}
	ds.broadcastLog(broadcaster, deploymentID, "success", "Terraform initialized successfully", "terraform")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Applying Terraform (this may take a few minutes)...", "terraform")
	if err := azure.ApplyTerraform(terraformDir); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to apply terraform: %v", err), "terraform")
		return "", fmt.Errorf("failed to apply terraform: %v", err)
	}
	ds.broadcastLog(broadcaster, deploymentID, "success", "Terraform applied successfully", "terraform")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Retrieving public IP address...", "network")
	publicIP, err := azure.GetTerraformOutput(terraformDir, "public_ip")
	if err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to get public IP: %v", err), "network")
		return "", fmt.Errorf("failed to get public IP: %v", err)
	}

	publicIP = strings.TrimSpace(publicIP)
	if publicIP == "" {
		ds.broadcastLog(broadcaster, deploymentID, "error", "Public IP is empty", "network")
		return "", fmt.Errorf("public IP is empty")
	}

	ds.broadcastLog(broadcaster, deploymentID, "success", fmt.Sprintf("Retrieved public IP: %s", publicIP), "network")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Verifying SSH keys...", "ssh")
	azurePrivateKeyPath := filepath.Join(terraformDir, "azure_vm_key")
	azurePublicKeyPath := filepath.Join(terraformDir, "azure_vm_key.pub")

	if _, err := os.Stat(azurePrivateKeyPath); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Private key not found at %s: %v", azurePrivateKeyPath, err), "ssh")
		return "", fmt.Errorf("Private key not found at %s: %v", azurePrivateKeyPath, err)
	}
	if _, err := os.Stat(azurePublicKeyPath); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Public key not found at %s: %v", azurePublicKeyPath, err), "ssh")
		return "", fmt.Errorf("Public key not found at %s: %v", azurePublicKeyPath, err)
	}

	ds.broadcastLog(broadcaster, deploymentID, "success", "SSH keys verified successfully", "ssh")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Creating Ansible configuration files...", "ansible")
	if err := ds.createAnsibleFiles(ansibleDir, req, publicIP, azurePrivateKeyPath); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to create ansible files: %v", err), "ansible")
		return "", fmt.Errorf("failed to create ansible files: %v", err)
	}
	ds.broadcastLog(broadcaster, deploymentID, "success", "Ansible files created successfully", "ansible")

	ds.broadcastLog(broadcaster, deploymentID, "info", "Waiting for VM to be ready (60 seconds)...", "vm")
	time.Sleep(60 * time.Second)

	ds.broadcastLog(broadcaster, deploymentID, "info", "Testing SSH connectivity...", "ssh")
	if err := ds.testSSHConnectivity(publicIP, azurePrivateKeyPath, broadcaster, deploymentID); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "warn", fmt.Sprintf("SSH connectivity test failed, but continuing: %v", err), "ssh")
		time.Sleep(30 * time.Second)
	} else {
		ds.broadcastLog(broadcaster, deploymentID, "success", "SSH connectivity test passed", "ssh")
	}

	ds.broadcastLog(broadcaster, deploymentID, "info", "Running Ansible playbook (this may take several minutes)...", "ansible")
	if err := ds.runAnsiblePlaybook(ansibleDir); err != nil {
		ds.broadcastLog(broadcaster, deploymentID, "error", fmt.Sprintf("Failed to run ansible playbook: %v", err), "ansible")
		return "", fmt.Errorf("failed to run ansible playbook: %v", err)
	}
	ds.broadcastLog(broadcaster, deploymentID, "success", "Ansible playbook execution completed successfully", "ansible")

	if req.AutoDeploy {
		ds.broadcastLog(broadcaster, deploymentID, "info", "Setting up GitHub Actions auto-deployment...", "github")
		if err := ds.setupGitHubActionsOnServer(ansibleDir, req, publicIP, terraformDir, broadcaster, deploymentID); err != nil {
			ds.broadcastLog(broadcaster, deploymentID, "warn", fmt.Sprintf("Failed to setup GitHub Actions: %v", err), "github")
		} else {
			ds.broadcastLog(broadcaster, deploymentID, "success", "GitHub Actions setup completed", "github")
		}

		ds.broadcastLog(broadcaster, deploymentID, "info", "Running additional GitHub Actions setup tasks...", "github")
		if err := ds.runAdditionalAnsibleTasks(ansibleDir, broadcaster, deploymentID); err != nil {
			ds.broadcastLog(broadcaster, deploymentID, "warn", fmt.Sprintf("Failed to run GitHub Actions setup tasks: %v", err), "github")
		} else {
			ds.broadcastLog(broadcaster, deploymentID, "success", "Additional GitHub Actions tasks completed", "github")
		}
	}

	ds.broadcastLog(broadcaster, deploymentID, "success", "Deployment completed successfully!", "completed")
	return publicIP, nil
}

func (ds *DeploymentService) testSSHConnectivity(publicIP, privateKeyPath string, broadcaster LogBroadcaster, deploymentID string) error {
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

	ds.broadcastLog(broadcaster, deploymentID, "info", fmt.Sprintf("SSH test output: %s", string(output)), "ssh")
	return nil
}
