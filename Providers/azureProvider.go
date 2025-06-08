package providers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"	
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/ssh"
)

// LogMessage represents a log entry
type LogMessage struct {
	Level     string
	Message   string
	Timestamp string
	Step      string
}

// LogBroadcaster interface for broadcasting logs
type LogBroadcaster interface {
	BroadcastLog(deploymentID string, logMsg LogMessage)
}

func init() {
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Warning: Error loading .env file: %v\n", err)
	}
}

type AzureProvider struct {
	ResourceGroup    string
	Location         string
	VMSize           string
	VMName           string
	Path_            string
	PublicKeyPath    string
	PublicKeyContent string
	broadcaster      LogBroadcaster
	deploymentID     string
}

func (a *AzureProvider) SetLogger(broadcaster LogBroadcaster, deploymentID string) {
	a.broadcaster = broadcaster
	a.deploymentID = deploymentID
}

func (a *AzureProvider) broadcastLog(level, message, step string) {
	// Create log message
	logMsg := LogMessage{
		Level:     level,
		Message:   message,
		Step:      step,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	
	// Print to console with color coding
	a.printLog(logMsg)
	
	// Broadcast to external logger if available
	if a.broadcaster != nil {
		a.broadcaster.BroadcastLog(a.deploymentID, logMsg)
	}
}

func (a *AzureProvider) printLog(logMsg LogMessage) {
	timestamp := time.Now().Format("15:04:05")
	
	// Color codes for different log levels
	var colorCode string
	var levelPrefix string
	
	switch strings.ToLower(logMsg.Level) {
	case "error":
		colorCode = "\033[31m" // Red
		levelPrefix = "ERROR"
	case "warning", "warn":
		colorCode = "\033[33m" // Yellow
		levelPrefix = "WARN "
	case "success":
		colorCode = "\033[32m" // Green
		levelPrefix = "SUCCESS"
	case "info":
		colorCode = "\033[36m" // Cyan
		levelPrefix = "INFO"
	case "debug":
		colorCode = "\033[37m" // White
		levelPrefix = "DEBUG"
	default:
		colorCode = "\033[0m"  // Reset
		levelPrefix = "LOG  "
	}
	
	resetCode := "\033[0m" // Reset color
	
	// Format: [timestamp] [LEVEL] [step] message
	fmt.Printf("%s[%s] [%s] [%s] %s%s\n", 
		colorCode, 
		timestamp, 
		levelPrefix, 
		strings.ToUpper(logMsg.Step), 
		logMsg.Message, 
		resetCode)
}

const azureTfTemplate = `
provider "azurerm" {
  features {}
  subscription_id = "${var.subscription_id}"
}

variable "subscription_id" {
  description = "Azure subscription ID"
  type        = string
}

# Local file resources for SSH keys
resource "local_file" "private_key" {
  content         = var.private_key_content
  filename        = "${path.module}/azure_vm_key"
  file_permission = "0600"
}

resource "local_file" "public_key" {
  content         = var.public_key_content
  filename        = "${path.module}/azure_vm_key.pub"
  file_permission = "0644"
}

# Variables for SSH key content
variable "private_key_content" {
  description = "Private SSH key content"
  type        = string
  sensitive   = true
}

variable "public_key_content" {
  description = "Public SSH key content"
  type        = string
}

resource "azurerm_resource_group" "example" {
  name     = "{{ .ResourceGroup }}"
  location = "{{ .Location }}"
}

resource "azurerm_virtual_network" "example" {
  name                = "example-network"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
}

resource "azurerm_subnet" "example" {
  name                 = "example-subnet"
  resource_group_name  = azurerm_resource_group.example.name
  virtual_network_name = azurerm_virtual_network.example.name
  address_prefixes     = ["10.0.2.0/24"]
}

resource "azurerm_public_ip" "example" {
  name                = "example-public-ip"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  allocation_method   = "Static"
  sku                 = "Standard"
}

resource "azurerm_network_security_group" "example" {
  name                = "example-security-group"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  # SSH access - restricted to specific port with rate limiting
  security_rule {
    name                       = "SSH"
    priority                   = 1001
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = "*"  # Consider restricting to your IP range
    destination_address_prefix = "*"
  }

  # HTTP access
  security_rule {
    name                       = "HTTP"
    priority                   = 1003
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  # HTTPS access
  security_rule {
    name                       = "HTTPS"
    priority                   = 1004
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  # Custom application port
  security_rule {
    name                       = "HTTP_8000"
    priority                   = 1002
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "8000"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  # Deny all other inbound traffic
  security_rule {
    name                       = "DenyAllInbound"
    priority                   = 4096
    direction                  = "Inbound"
    access                     = "Deny"
    protocol                   = "*"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_interface" "example" {
  name                = "example-nic"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.example.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.example.id
  }
}

resource "azurerm_network_interface_security_group_association" "example" {
  network_interface_id      = azurerm_network_interface.example.id
  network_security_group_id = azurerm_network_security_group.example.id
}

resource "azurerm_linux_virtual_machine" "example" {
  name                = "{{ .VMName }}"
  resource_group_name = azurerm_resource_group.example.name
  location            = azurerm_resource_group.example.location
  size                = "{{ .VMSize }}"
  admin_username      = "azureuser"
  network_interface_ids = [azurerm_network_interface.example.id]
  
  # Enhanced Security: Disable password authentication
  disable_password_authentication = true

  # SSH Key Configuration - using the variable content directly
  admin_ssh_key {
    username   = "azureuser"
    public_key = var.public_key_content
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
    disk_size_gb         = 30
    # Enable encryption at host for additional security
    secure_vm_disk_encryption_set_id = null
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts-gen2"
    version   = "latest"
  }

  # Security and monitoring tags
  tags = {
    Environment = "Development"
    AutoShutdown = "19:00"
    Security = "SSH-Keys-Only"
    Monitoring = "Enabled"
  }

  # Boot diagnostics for troubleshooting
  boot_diagnostics {
    storage_account_uri = null  # Uses managed storage account
  }

  # Ensure SSH keys are created before VM
  depends_on = [local_file.private_key, local_file.public_key]
}

# Auto-shutdown schedule (helps save costs on free trial)
resource "azurerm_dev_test_global_vm_shutdown_schedule" "example" {
  virtual_machine_id = azurerm_linux_virtual_machine.example.id
  location           = azurerm_resource_group.example.location
  enabled            = true

  daily_recurrence_time = "1900"
  timezone              = "UTC"

  notification_settings {
    enabled = false
  }

  tags = {
    Environment = "Development"
  }
}

# Outputs
output "public_ip" {
  value = azurerm_public_ip.example.ip_address
  depends_on = [azurerm_linux_virtual_machine.example]
}

output "resource_group" {
  value = azurerm_resource_group.example.name
}

output "vm_name" {
  value = azurerm_linux_virtual_machine.example.name
}

output "ssh_connection_command" {
  value = "ssh -i ${path.cwd}/azure_vm_key azureuser@${azurerm_public_ip.example.ip_address}"
  depends_on = [azurerm_linux_virtual_machine.example]
}
`

func generateSSHKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %v", err)
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %v", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	return string(publicKeyBytes), string(privateKeyBytes), nil
}

func (a *AzureProvider) GenerateSSHKeys(path string) (string, string, error) {
	a.broadcastLog("info", "Generating SSH key pair...", "ssh")
	publicKeyContent, privateKeyContent, err := generateSSHKeyPair()
	if err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to generate SSH keys: %v", err), "ssh")
		return "", "", fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	a.PublicKeyContent = publicKeyContent
	a.broadcastLog("info", "Writing SSH keys to files...", "ssh")

	publicKeyPath := filepath.Join(path, "azure_vm_key.pub")
	if err := os.WriteFile(publicKeyPath, []byte(publicKeyContent), 0644); err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to write public key: %v", err), "ssh")
		return "", "", fmt.Errorf("failed to write public key: %v", err)
	}

	privateKeyPath := filepath.Join(path, "azure_vm_key")
	if err := os.WriteFile(privateKeyPath, []byte(privateKeyContent), 0600); err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to write private key: %v", err), "ssh")
		return "", "", fmt.Errorf("failed to write private key: %v", err)
	}

	a.PublicKeyPath = publicKeyPath
	a.broadcastLog("success", fmt.Sprintf("SSH keys generated successfully at %s", path), "ssh")

	return publicKeyContent, privateKeyContent, nil
}

func (a *AzureProvider) GenerateTerraformConfig(path string) error {
	a.broadcastLog("info", "Generating Terraform configuration...", "terraform")

	publicKeyContent, privateKeyContent, err := a.GenerateSSHKeys(path)
	if err != nil {
		return err
	}

	filePath := filepath.Join(path, "main.tf")
	file, err := os.Create(filePath)
	if err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to create main.tf: %v", err), "terraform")
		return err
	}
	defer file.Close()

	tmpl, err := template.New("azure").Parse(azureTfTemplate)
	if err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to parse Terraform template: %v", err), "terraform")
		return err
	}

	if err := tmpl.Execute(file, a); err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to execute Terraform template: %v", err), "terraform")
		return err
	}

	a.broadcastLog("info", "Reading Azure subscription ID from environment...", "terraform")
	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		a.broadcastLog("error", "AZURE_SUBSCRIPTION_ID environment variable is not set", "terraform")
		return fmt.Errorf("AZURE_SUBSCRIPTION_ID environment variable is not set")
	}

	tfvarsPath := filepath.Join(path, "terraform.tfvars")
	tfvarsContent := fmt.Sprintf(`subscription_id = %q
private_key_content = %q
public_key_content = %q
`, subscriptionID, privateKeyContent, publicKeyContent)

	if err := os.WriteFile(tfvarsPath, []byte(tfvarsContent), 0600); err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to write terraform.tfvars: %v", err), "terraform")
		return fmt.Errorf("failed to write terraform.tfvars: %v", err)
	}

	a.broadcastLog("success", "Terraform configuration generated successfully", "terraform")
	return nil
}

func (a *AzureProvider) InitTerraform(path string) error {
	a.broadcastLog("info", "Initializing Terraform...", "terraform")
	cmd := exec.Command("terraform", "init")
	cmd.Dir = path

	output, err := cmd.CombinedOutput()
	if err != nil {
		a.broadcastLog("error", fmt.Sprintf("Terraform init failed: %v\nOutput: %s", err, string(output)), "terraform")
		return fmt.Errorf("terraform init failed: %v", err)
	}

	a.broadcastLog("debug", fmt.Sprintf("Terraform init output:\n%s", string(output)), "terraform")
	a.broadcastLog("success", "Terraform initialized successfully", "terraform")
	return nil
}

func (a *AzureProvider) ApplyTerraform(path string) error {
	a.broadcastLog("info", "Applying Terraform configuration (this may take a few minutes)...", "terraform")
	
	// Show progress indicator
	fmt.Print("Deploying resources")
	go func() {
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			fmt.Print(".")
		}
	}()
	
	cmd := exec.Command("terraform", "apply", "-auto-approve")
	cmd.Dir = path

	output, err := cmd.CombinedOutput()
	fmt.Println() // New line after progress dots
	
	if err != nil {
		a.broadcastLog("error", fmt.Sprintf("Terraform apply failed: %v\nOutput: %s", err, string(output)), "terraform")
		return fmt.Errorf("terraform apply failed: %v", err)
	}

	// Parse and show important outputs from terraform apply
	outputStr := string(output)
	if strings.Contains(outputStr, "Apply complete!") {
		a.broadcastLog("success", "Infrastructure deployment completed successfully", "terraform")
	}
	
	a.broadcastLog("debug", fmt.Sprintf("Terraform apply output:\n%s", outputStr), "terraform")
	return nil
}

func (a *AzureProvider) GetTerraformOutput(path, key string) (string, error) {
	a.broadcastLog("info", fmt.Sprintf("Getting Terraform output for key: %s", key), "terraform")
	cmd := exec.Command("terraform", "output", "-raw", key)
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to get Terraform output %s: %v", key, err), "terraform")
		return "", err
	}

	value := strings.TrimSpace(string(output))
	a.broadcastLog("success", fmt.Sprintf("Retrieved %s: %s", key, value), "terraform")
	return value, nil
}

func (a *AzureProvider) WriteInventory(path, ip string) error {
	a.broadcastLog("info", "Writing Ansible inventory file...", "ansible")
	privateKeyPath := filepath.Join(path, "azure_vm_key")
	content := fmt.Sprintf(`[azure]
%s ansible_user=azureuser ansible_ssh_private_key_file=%s ansible_connection=ssh ansible_ssh_common_args='-o StrictHostKeyChecking=no'
`, ip, privateKeyPath)

	if err := os.WriteFile(filepath.Join(path, "inventory.ini"), []byte(content), 0644); err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to write inventory file: %v", err), "ansible")
		return err
	}

	a.broadcastLog("success", "Ansible inventory file created successfully", "ansible")
	return nil
}

func (a *AzureProvider) CreateSecurityAuditScript(path string) error {
	a.broadcastLog("info", "Creating security audit script...", "security")
	script := `#!/bin/bash
# Azure VM Security Audit Script

echo "=== Azure VM Security Audit ==="
echo "Timestamp: $(date)"
echo

echo "1. Checking SSH configuration..."
sudo grep -E "^(PasswordAuthentication|PermitRootLogin|Protocol)" /etc/ssh/sshd_config || echo "SSH config check completed"
echo

echo "2. Checking active network connections..."
sudo netstat -tuln
echo

echo "3. Checking firewall status..."
sudo ufw status verbose
echo

echo "4. Checking for automatic updates..."
sudo apt list --upgradable
echo

echo "5. Checking system logs for authentication attempts..."
sudo grep "authentication failure\|Failed password" /var/log/auth.log | tail -10
echo

echo "=== Audit Complete ==="
`
	scriptPath := filepath.Join(path, "security_audit.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		a.broadcastLog("error", fmt.Sprintf("Failed to create security audit script: %v", err), "security")
		return fmt.Errorf("failed to create security audit script: %v", err)
	}

	a.broadcastLog("success", "Security audit script created successfully", "security")
	return nil
}

// PrintDeploymentSummary prints a formatted summary of the deployment
func (a *AzureProvider) PrintDeploymentSummary(path string) error {
	a.broadcastLog("info", "Generating deployment summary...", "summary")
	
	// Get outputs
	publicIP, _ := a.GetTerraformOutput(path, "public_ip")
	resourceGroup, _ := a.GetTerraformOutput(path, "resource_group")
	vmName, _ := a.GetTerraformOutput(path, "vm_name")
	sshCommand, _ := a.GetTerraformOutput(path, "ssh_connection_command")
	
	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("🚀 AZURE DEPLOYMENT SUMMARY\n")
	fmt.Printf(strings.Repeat("=", 60) + "\n")
	fmt.Printf("📍 Resource Group: %s\n", resourceGroup)
	fmt.Printf("💻 VM Name: %s\n", vmName)
	fmt.Printf("📍 Location: %s\n", a.Location)
	fmt.Printf("📊 VM Size: %s\n", a.VMSize)
	fmt.Printf("🌐 Public IP: %s\n", publicIP)
	fmt.Printf(strings.Repeat("-", 60) + "\n")
	fmt.Printf("🔑 SSH Connection:\n")
	fmt.Printf("   %s\n", sshCommand)
	fmt.Printf(strings.Repeat("-", 60) + "\n")
	fmt.Printf("📁 Generated Files:\n")
	fmt.Printf("   • main.tf (Terraform configuration)\n")
	fmt.Printf("   • terraform.tfvars (Variables)\n")
	fmt.Printf("   • azure_vm_key (Private SSH key)\n")
	fmt.Printf("   • azure_vm_key.pub (Public SSH key)\n")
	fmt.Printf("   • inventory.ini (Ansible inventory)\n")
	fmt.Printf("   • security_audit.sh (Security audit script)\n")
	fmt.Printf(strings.Repeat("=", 60) + "\n\n")
	
	a.broadcastLog("success", "Deployment completed successfully!", "summary")
	return nil
}

func NewMinimalAzureProvider(resourceGroup, vmName string) *AzureProvider {
	return &AzureProvider{
		ResourceGroup: resourceGroup,
		Location:      "East US",
		VMSize:        "Standard_B1ls",
		VMName:        vmName,
	}
}

func NewAzureProviderB1s(resourceGroup, vmName string) *AzureProvider {
	return &AzureProvider{
		ResourceGroup: resourceGroup,
		Location:      "East US",
		VMSize:        "Standard_B1s",
		VMName:        vmName,
	}
}