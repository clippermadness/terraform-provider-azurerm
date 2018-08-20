package azurerm

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2018-04-01/network"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/response"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

var routeTableResourceName = "azurerm_route_table"

func resourceArmRouteTable() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmRouteTableCreateUpdate,
		Read:   resourceArmRouteTableRead,
		Update: resourceArmRouteTableCreateUpdate,
		Delete: resourceArmRouteTableDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(time.Minute * 30),
			Update: schema.DefaultTimeout(time.Minute * 30),
			Delete: schema.DefaultTimeout(time.Minute * 30),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.NoZeroValues,
			},

			"location": locationSchema(),

			"resource_group_name": resourceGroupNameSchema(),

			"route": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.NoZeroValues,
						},

						"address_prefix": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.NoZeroValues,
						},

						"next_hop_type": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(network.RouteNextHopTypeVirtualNetworkGateway),
								string(network.RouteNextHopTypeVnetLocal),
								string(network.RouteNextHopTypeInternet),
								string(network.RouteNextHopTypeVirtualAppliance),
								string(network.RouteNextHopTypeNone),
							}, true),
							DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
						},

						"next_hop_in_ip_address": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ValidateFunc: validation.NoZeroValues,
						},
					},
				},
			},

			"disable_bgp_route_propagation": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},

			"subnets": {
				Type:     schema.TypeSet,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},

			"tags": tagsSchema(),
		},
	}
}

func resourceArmRouteTableCreateUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).routeTablesClient
	ctx := meta.(*ArmClient).StopContext

	log.Printf("[INFO] preparing arguments for AzureRM Route Table creation.")

	name := d.Get("name").(string)
	resGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		// first check if there's one in this subscription requiring import
		resp, err := client.Get(ctx, resGroup, name, "")
		if err != nil {
			if !utils.ResponseWasNotFound(resp.Response) {
				return fmt.Errorf("Error checking for the existence of Route Table %q (Resource Group %q): %+v", name, resGroup, err)
			}
		}

		if resp.ID != nil {
			return tf.ImportAsExistsError("azurerm_route_table", *resp.ID)
		}
	}

	location := azureRMNormalizeLocation(d.Get("location").(string))
	tags := d.Get("tags").(map[string]interface{})

	routes, err := expandRouteTableRoutes(d)
	if err != nil {
		return fmt.Errorf("Error Expanding list of Route Table Routes: %+v", err)
	}

	routeSet := network.RouteTable{
		Name:     &name,
		Location: &location,
		RouteTablePropertiesFormat: &network.RouteTablePropertiesFormat{
			Routes: &routes,
			DisableBgpRoutePropagation: utils.Bool(d.Get("disable_bgp_route_propagation").(bool)),
		},
		Tags: expandTags(tags),
	}

	future, err := client.CreateOrUpdate(ctx, resGroup, name, routeSet)
	if err != nil {
		return fmt.Errorf("Error Creating/Updating Route Table %q (Resource Group %q): %+v", name, resGroup, err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(tf.TimeoutForCreateUpdate(d)))
	defer cancel()
	err = future.WaitForCompletionRef(waitCtx, client.Client)
	if err != nil {
		return fmt.Errorf("Error waiting for completion of Route Table %q (Resource Group %q): %+v", name, resGroup, err)
	}

	read, err := client.Get(ctx, resGroup, name, "")
	if err != nil {
		return err
	}
	if read.ID == nil {
		return fmt.Errorf("Cannot read Route Table %q (resource group %q) ID", name, resGroup)
	}

	d.SetId(*read.ID)

	return resourceArmRouteTableRead(d, meta)
}

func resourceArmRouteTableRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).routeTablesClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resGroup := id.ResourceGroup
	name := id.Path["routeTables"]

	resp, err := client.Get(ctx, resGroup, name, "")
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error making Read request on Azure Route Table %q: %+v", name, err)
	}

	d.Set("name", name)
	d.Set("resource_group_name", resGroup)
	if location := resp.Location; location != nil {
		d.Set("location", azureRMNormalizeLocation(*location))
	}

	if props := resp.RouteTablePropertiesFormat; props != nil {
		d.Set("disable_bgp_route_propagation", props.DisableBgpRoutePropagation)
		if err := d.Set("route", flattenRouteTableRoutes(props.Routes)); err != nil {
			return err
		}

		if err := d.Set("subnets", flattenRouteTableSubnets(props.Subnets)); err != nil {
			return err
		}
	}

	flattenAndSetTags(d, resp.Tags)

	return nil
}

func resourceArmRouteTableDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).routeTablesClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resGroup := id.ResourceGroup
	name := id.Path["routeTables"]

	future, err := client.Delete(ctx, resGroup, name)
	if err != nil {
		if !response.WasNotFound(future.Response()) {
			return fmt.Errorf("Error deleting Route Table %q (Resource Group %q): %+v", name, resGroup, err)
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutDelete))
	defer cancel()
	err = future.WaitForCompletionRef(waitCtx, client.Client)
	if err != nil {
		return fmt.Errorf("Error waiting for deletion of Route Table %q (Resource Group %q): %+v", name, resGroup, err)
	}

	return nil
}

func expandRouteTableRoutes(d *schema.ResourceData) ([]network.Route, error) {
	configs := d.Get("route").([]interface{})
	routes := make([]network.Route, 0, len(configs))

	for _, configRaw := range configs {
		data := configRaw.(map[string]interface{})

		addressPrefix := data["address_prefix"].(string)
		nextHopType := data["next_hop_type"].(string)

		properties := network.RoutePropertiesFormat{
			AddressPrefix: &addressPrefix,
			NextHopType:   network.RouteNextHopType(nextHopType),
		}

		if v := data["next_hop_in_ip_address"].(string); v != "" {
			properties.NextHopIPAddress = &v
		}

		name := data["name"].(string)
		route := network.Route{
			Name: &name,
			RoutePropertiesFormat: &properties,
		}

		routes = append(routes, route)
	}

	return routes, nil
}

func flattenRouteTableRoutes(input *[]network.Route) []interface{} {
	results := make([]interface{}, 0)

	if routes := input; routes != nil {
		for _, route := range *routes {
			r := make(map[string]interface{})

			r["name"] = *route.Name

			if props := route.RoutePropertiesFormat; props != nil {
				r["address_prefix"] = *props.AddressPrefix
				r["next_hop_type"] = string(props.NextHopType)
				if ip := props.NextHopIPAddress; ip != nil {
					r["next_hop_in_ip_address"] = *ip
				}
			}

			results = append(results, r)
		}
	}

	return results
}

func flattenRouteTableSubnets(input *[]network.Subnet) []string {
	output := []string{}

	if subnets := input; subnets != nil {
		for _, subnet := range *subnets {
			output = append(output, *subnet.ID)
		}
	}

	return output
}
