package providers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/ssh"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

type AzureProvider struct {
	ResourceGroup string
	Location      string
	VMSize        string
	VMName        string
	Path_         string
	PublicKeyPath string
	PublicKeyContent string // Add this field to store the actual key content
}

// Updated Terraform template - using local_file resource and public key content directly
const azureTfTemplate = `
provider "azurerm" {
  features {}
  subscription_id = "145296c1-8df7-4169-9ff3-7858d9aeea61"
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

	// Generate public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %v", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	return string(publicKeyBytes), string(privateKeyBytes), nil
}

func (a *AzureProvider) GenerateSSHKeys(path string) (string, string, error) {
	publicKeyContent, privateKeyContent, err := generateSSHKeyPair()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	// Store the content in the struct for use in Terraform variables
	a.PublicKeyContent = publicKeyContent

	// Also write to files for direct access
	publicKeyPath := filepath.Join(path, "azure_vm_key.pub")
	if err := os.WriteFile(publicKeyPath, []byte(publicKeyContent), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write public key: %v", err)
	}

	privateKeyPath := filepath.Join(path, "azure_vm_key")
	if err := os.WriteFile(privateKeyPath, []byte(privateKeyContent), 0600); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %v", err)
	}

	a.PublicKeyPath = publicKeyPath

	fmt.Printf("SSH keys generated successfully:\n")
	fmt.Printf("  Private key: %s\n", privateKeyPath)
	fmt.Printf("  Public key: %s\n", publicKeyPath)
	fmt.Printf("\nTo connect to the VM, use: ssh -i %s azureuser@<VM_IP>\n", privateKeyPath)

	return publicKeyContent, privateKeyContent, nil
}

func (a *AzureProvider) GenerateTerraformConfig(path string) error {
	// Generate SSH keys first and get the content
	publicKeyContent, privateKeyContent, err := a.GenerateSSHKeys(path)
	if err != nil {
		return err
	}

	// Create main.tf file
	filePath := filepath.Join(path, "main.tf")
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	tmpl, err := template.New("azure").Parse(azureTfTemplate)
	if err != nil {
		return err
	}

	if err := tmpl.Execute(file, a); err != nil {
		return err
	}

	// Create terraform.tfvars file with SSH key content
	tfvarsPath := filepath.Join(path, "terraform.tfvars")
	tfvarsContent := fmt.Sprintf(`private_key_content = %q
public_key_content = %q
`, privateKeyContent, publicKeyContent)

	if err := os.WriteFile(tfvarsPath, []byte(tfvarsContent), 0600); err != nil {
		return fmt.Errorf("failed to write terraform.tfvars: %v", err)
	}

	return nil
}

func (a *AzureProvider) InitTerraform(path string) error {
	cmd := exec.Command("terraform", "init")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *AzureProvider) ApplyTerraform(path string) error {
	cmd := exec.Command("terraform", "apply", "-auto-approve")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *AzureProvider) GetTerraformOutput(path, key string) (string, error) {
	cmd := exec.Command("terraform", "output", "-raw", key)
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *AzureProvider) WriteInventory(path, ip string) error {
	privateKeyPath := filepath.Join(path, "azure_vm_key")
	content := fmt.Sprintf(`[azure]
%s ansible_user=azureuser ansible_ssh_private_key_file=%s ansible_connection=ssh ansible_ssh_common_args='-o StrictHostKeyChecking=no'
`, ip, privateKeyPath)
	return os.WriteFile(filepath.Join(path, "inventory.ini"), []byte(content), 0644)
}

// CreateSecurityAuditScript creates a script to audit VM security
func (a *AzureProvider) CreateSecurityAuditScript(path string) error {
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
		return fmt.Errorf("failed to create security audit script: %v", err)
	}
	
	fmt.Printf("Security audit script created: %s\n", scriptPath)
	return nil
}

// Helper function to create a minimal Azure provider instance optimized for free tier
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