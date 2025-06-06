package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"sathwikshetty33/Django-vpc/Services"
)

type DeploymentResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	PublicIP  string `json:"public_ip,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

func main() {
	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})

	r.POST("/deploy", handleDeployment)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339)})
	})

	log.Println("Starting server on :8080...")
	r.Run(":8080")
}

func handleDeployment(c *gin.Context) {
	var req services.DeploymentRequest
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, DeploymentResponse{
			Success:   false,
			Error:     fmt.Sprintf("Invalid request body: %v", err),
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	if err := validateRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, DeploymentResponse{
			Success:   false,
			Error:     err.Error(),
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	deploymentService := services.NewDeploymentService()
	
	publicIP, err := deploymentService.Deploy(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, DeploymentResponse{
			Success:   false,
			Error:     err.Error(),
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	c.JSON(http.StatusOK, DeploymentResponse{
		Success:   true,
		Message:   "Deployment completed successfully",
		PublicIP:  publicIP,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func validateRequest(req *services.DeploymentRequest) error {
	if req.Username == "" {
		return fmt.Errorf("username is required")
	}
	if req.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	if req.GithubToken == "" {
		return fmt.Errorf("github_token is required")
	}

	return nil
}