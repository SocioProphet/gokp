package cmd

import (
	"os"

	"github.com/christianh814/gokp/cmd/argo"
	"github.com/christianh814/gokp/cmd/capi"
	"github.com/christianh814/gokp/cmd/export"
	"github.com/christianh814/gokp/cmd/flux"

	"github.com/christianh814/gokp/cmd/github"
	"github.com/christianh814/gokp/cmd/kind"
	"github.com/christianh814/gokp/cmd/templates"
	"github.com/christianh814/gokp/cmd/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// azurecreateCmd represents the azure create command
var azurecreateCmd = &cobra.Command{
	Use:   "azure",
	Short: "Creates a GOKP Cluster on azure",
	Long: `Create a GOKP Cluster on azure. This will build a cluster on azure using the given
credentials. For example:

//todo: change
gokp create-cluster azure --cluster-name=mycluster \
--github-token=githubtoken \
--azure-app-id='app-id' \
--azure-app-secret='app-secret' \
--azure-tenant-id='tenant-id' \
--azure-subscription-id='subscription-id' \
--azure-resource-group='rg-name'
--private-repo=true`,
	Run: func(cmd *cobra.Command, args []string) {
		// create home dir
		err := os.MkdirAll(os.Getenv("HOME")+"/.gokp", 0775)
		if err != nil {
			log.Fatal(err)
		}
		// Create workdir and set variables based on that
		WorkDir, _ = utils.CreateWorkDir()
		KindCfg = WorkDir + "/" + "kind.kubeconfig"
		// cleanup workdir at the end
		defer os.RemoveAll(WorkDir)

		// Grab repo related flags
		ghToken, _ := cmd.Flags().GetString("github-token")
		clusterName, _ := cmd.Flags().GetString("cluster-name")
		privateRepo, _ := cmd.Flags().GetBool("private-repo")

		// Set GitOps Controller
		gitOpsController, _ := cmd.Flags().GetString("gitops-controller")

		// Grab Azure related flags
		azureRegion, _ := cmd.Flags().GetString("azure-region")
		azureAppId, _ := cmd.Flags().GetString("azure-app-id")
		azureAppSecret, _ := cmd.Flags().GetString("azure-app-secret")
		azureTenantId, _ := cmd.Flags().GetString("azure-tenant-id")
		azureSubscriptionId, _ := cmd.Flags().GetString("azure-subscription-id")
		azureSSHKey, _ := cmd.Flags().GetString("azure-ssh-key")
		azureCPMachine, _ := cmd.Flags().GetString("azure-control-plane-machine")
		azureWMachine, _ := cmd.Flags().GetString("azure-node-machine")
		azureResourceGroup, _ := cmd.Flags().GetString("azure-resource-group")

		CapiCfg := WorkDir + "/" + clusterName + ".kubeconfig"
		gokpartifacts := os.Getenv("HOME") + "/.gokp/" + clusterName

		tcpName := "gokp-bootstrapper"

		// Run PreReq Checks
		_, err = utils.CheckPreReqs(gokpartifacts, gitOpsController)
		if err != nil {
			log.Fatal(err)
		}

		// Create KIND instance
		log.Info("entering Azure command")
		log.Info("Creating temporary control plane")
		err = kind.CreateKindCluster(tcpName, KindCfg)
		if err != nil {
			log.Fatal(err)
		}

		// Create CAPI instance on AWS
		azureCredsMap := map[string]string{
			"AZURE_LOCATION":                   azureRegion,
			"AZURE_CLIENT_ID":                  azureAppId,
			"AZURE_CLIENT_SECRET":              azureAppSecret,
			"AZURE_TENANT_ID":                  azureTenantId,
			"AZURE_SUBSCRIPTION_ID":            azureSubscriptionId,
			"AZURE_CONTROL_PLANE_MACHINE_TYPE": azureCPMachine,
			"AZURE_NODE_MACHINE_TYPE":          azureWMachine,
			"AZURE_SSH_KEY":                    azureSSHKey,
			"AZURE_RESOURCE_GROUP":             azureResourceGroup,
		}

		// By default, create an HA Cluster
		haCluster := true
		_, err = capi.CreateAzureK8sInstance(KindCfg, &clusterName, WorkDir, azureCredsMap, CapiCfg, haCluster)
		if err != nil {
			log.Fatal(err)
		}

		// Create the GitOps repo
		_, gitopsrepo, err := github.CreateRepo(&clusterName, ghToken, &privateRepo, WorkDir)
		if err != nil {
			log.Fatal(err)
		}

		// Create repo dir structure based on which gitops controller that was chosen
		if gitOpsController == "argocd" {
			// Create repo dir structure. Including Argo CD install YAMLs and base YAMLs. Push initial dir structure out
			_, err = templates.CreateArgoRepoSkel(&clusterName, WorkDir, ghToken, gitopsrepo, &privateRepo)
			if err != nil {
				log.Fatal(err)
			}
		} else if gitOpsController == "fluxcd" {
			// Create repo dir structure. Including Flux CD install YAMLs and base YAMLs. Push initial dir structure out
			_, err = templates.CreateFluxRepoSkel(&clusterName, WorkDir, ghToken, gitopsrepo, &privateRepo)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal("unknown gitops controller")
		}

		// Export/Create Cluster YAML to the Repo, Make sure kustomize is used for the core components
		log.Info("Exporting Cluster YAML")
		_, err = export.ExportClusterYaml(CapiCfg, WorkDir+"/"+clusterName, gitOpsController)
		if err != nil {
			log.Fatal(err)
		}

		// Git push newly exported YAML to GitOps repo
		privateKeyFile := WorkDir + "/" + clusterName + "_rsa"
		_, err = github.CommitAndPush(WorkDir+"/"+clusterName, privateKeyFile, "exporting existing YAML")
		if err != nil {
			log.Fatal(err)
		}

		// Deplopy the GitOps controller that was chosen
		if gitOpsController == "argocd" {
			// Install Argo CD on the newly created cluster with applications/applicationsets
			log.Info("Deploying Argo CD GitOps Controller")
			_, err = argo.BootstrapArgoCD(&clusterName, WorkDir, CapiCfg)
			if err != nil {
				log.Fatal(err)
			}
		} else if gitOpsController == "fluxcd" {
			// Install Flux CD on the newly created cluster with all it's components
			log.Info("Deploying Flux CD GitOps Controller")
			_, err = flux.BootstrapFluxCD(&clusterName, WorkDir, CapiCfg)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			log.Fatal("unknown gitops controller")
		}

		// MOVE from kind to capi instance
		log.Info("Moving CAPI Artifacts to: " + clusterName)
		_, err = capi.MoveMgmtCluster(KindCfg, CapiCfg, "capz")
		if err != nil {
			log.Fatal(err)
		}

		// Delete local Kind Cluster
		log.Info("Deleting temporary control plane")
		err = kind.DeleteKindCluster(tcpName, KindCfg)
		if err != nil {
			log.Fatal(err)
		}

		// Move components to ~/.gokp/<clustername> and remove stuff you don't need to know.
		// 	TODO: this is ugly and will refactor this later
		///err = utils.CopyDir(WorkDir, gokpartifacts)
		err = os.Rename(WorkDir, gokpartifacts)
		if err != nil {
			log.Fatal(err)
		}

		notNeeded := []string{
			"argocd-install-output",
			"capi-install-yamls-output",
			"cni-output",
			"fluxcd-install-output",
			"argocd-install.yaml",
			"flux-install.yaml",
			"cni.yaml",
			"install-cluster.yaml",
			"kind.kubeconfig",
		}

		for _, notNeededthing := range notNeeded {
			err = os.RemoveAll(gokpartifacts + "/" + notNeededthing)
			if err != nil {
				log.Fatal(err)
			}
		}

		// Give info
		log.Info("Cluster Successfully installed! Everything you need is under: ~/.gokp/", clusterName)

	},
}

func init() {
	createClusterCmd.AddCommand(azurecreateCmd)

	// GitOps Controller Flag
	azurecreateCmd.Flags().String("gitops-controller", "argocd", "The GitOps Controller to use for this cluster.")

	// Repo specific flags
	azurecreateCmd.Flags().String("github-token", "", "GitHub token to use.")
	azurecreateCmd.Flags().String("cluster-name", "", "Name of your cluster.")
	azurecreateCmd.Flags().BoolP("private-repo", "", true, "Create a private repo.")

	// Azure Specific flags
	azurecreateCmd.Flags().String("azure-region", "westus2", "Which region to deploy to.")
	azurecreateCmd.Flags().String("azure-app-id", "", "Your Azure app ID.")
	azurecreateCmd.Flags().String("azure-app-secret", "", "Your Azure Secret Key.")
	azurecreateCmd.Flags().String("azure-tenant-id", "", "Your Azure tenant ID.")
	azurecreateCmd.Flags().String("azure-subscription-id", "", "Your Azure subscription ID.")
	azurecreateCmd.Flags().String("azure-ssh-key", "default", "The SSH key in AWS that you want to use for the instances.")
	azurecreateCmd.Flags().String("azure-control-plane-machine", "Standard_D2s_v3", "The Azure VM type for the Control Plane")
	azurecreateCmd.Flags().String("azure-node-machine", "Standard_D2s_v3", "The Azure VM type for the Worker instances")
	azurecreateCmd.Flags().String("azure-resource-group", "gokp-cluster", "The Azure resource group name")

	// require the following flags
	azurecreateCmd.MarkFlagRequired("github-token")
	azurecreateCmd.MarkFlagRequired("cluster-name")
	azurecreateCmd.MarkFlagRequired("azure-app-id")
	azurecreateCmd.MarkFlagRequired("azure-app-secret")
	azurecreateCmd.MarkFlagRequired("azure-tenant-id")
	azurecreateCmd.MarkFlagRequired("azure-subscription-id")
}

//
