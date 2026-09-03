package phases

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/tags"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

// Phase13NatGateway provisions an Azure NAT Gateway and Public IP for outbound connectivity.
func Phase13NatGateway(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	if cl.Network.NATGateway != nil && !*cl.Network.NATGateway {
		fmt.Fprintln(os.Stderr, "[phase 13] nat-gateway disabled by intent — skipping")
		return nil
	}

	rgName := cl.Derived.ResourceGroupName
	pipName := cl.Derived.PublicIPName
	natName := cl.Derived.NatGatewayName
	location := cl.Metadata.Location

	fmt.Fprintf(os.Stderr, "[phase 13] nat-gateway: %s (public-ip: %s)\n", natName, pipName)

	if dryRun {
		st.Set("AZURE_NAT_GW_NAME", natName)
		st.Set("AZURE_NAT_PIP_NAME", pipName)
		st.Set("AZURE_NAT_PIP_ADDRESS", "198.51.100.1")
		fmt.Fprintf(os.Stderr, "[phase 13] [dry-run] would create NAT Gateway %s and Public IP %s\n", natName, pipName)
		return nil
	}

	pipTags := tags.ForCluster(cl.Metadata.Name, "pip", cl.Tags)
	natTags := tags.ForCluster(cl.Metadata.Name, "natgw", cl.Tags)

	skuStandard := armnetwork.PublicIPAddressSKUNameStandard
	allocStatic := armnetwork.IPAllocationMethodStatic

	pipPoller, err := clients.PublicIPAddresses.BeginCreateOrUpdate(ctx, rgName, pipName, armnetwork.PublicIPAddress{
		Location: &location,
		Tags:     pipTags,
		SKU: &armnetwork.PublicIPAddressSKU{
			Name: &skuStandard,
		},
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: &allocStatic,
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("phase13: BeginCreateOrUpdate PublicIP %s: %w", pipName, err)
	}

	pipResp, err := pipPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("phase13: waiting for PublicIP %s: %w", pipName, err)
	}

	if pipResp.ID != nil {
		st.Set("AZURE_NAT_PIP_ID", *pipResp.ID)
	}
	st.Set("AZURE_NAT_PIP_NAME", pipName)
	if pipResp.Properties != nil && pipResp.Properties.IPAddress != nil {
		st.Set("AZURE_NAT_PIP_ADDRESS", *pipResp.Properties.IPAddress)
		fmt.Fprintf(os.Stderr, "[phase 13] Allocated Public IP: %s\n", *pipResp.Properties.IPAddress)
	}

	natSkuStandard := armnetwork.NatGatewaySKUNameStandard
	natPoller, err := clients.NatGateways.BeginCreateOrUpdate(ctx, rgName, natName, armnetwork.NatGateway{
		Location: &location,
		Tags:     natTags,
		SKU: &armnetwork.NatGatewaySKU{
			Name: &natSkuStandard,
		},
		Properties: &armnetwork.NatGatewayPropertiesFormat{
			PublicIPAddresses: []*armnetwork.SubResource{
				{ID: pipResp.ID},
			},
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("phase13: BeginCreateOrUpdate NatGateway %s: %w", natName, err)
	}

	natResp, err := natPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("phase13: waiting for NatGateway %s: %w", natName, err)
	}

	if natResp.ID != nil {
		st.Set("AZURE_NAT_GW_ID", *natResp.ID)
	}
	st.Set("AZURE_NAT_GW_NAME", natName)

	if err := st.Save(); err != nil {
		return fmt.Errorf("phase13: saving state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[phase 13] NAT Gateway %s OK\n", natName)
	return nil
}
