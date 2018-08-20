package azurerm

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/servicebus/mgmt/2017-04-01/servicebus"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/azure"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

func resourceArmServiceBusTopicAuthorizationRule() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmServiceBusTopicAuthorizationRuleCreateUpdate,
		Read:   resourceArmServiceBusTopicAuthorizationRuleRead,
		Update: resourceArmServiceBusTopicAuthorizationRuleCreateUpdate,
		Delete: resourceArmServiceBusTopicAuthorizationRuleDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(time.Minute * 30),
			Update: schema.DefaultTimeout(time.Minute * 30),
			Delete: schema.DefaultTimeout(time.Minute * 30),
		},
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: azure.ServiceBusAuthorizationRuleSchemaFrom(map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: azure.ValidateServiceBusAuthorizationRuleName(),
			},

			"namespace_name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: azure.ValidateServiceBusNamespaceName(),
			},

			"topic_name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: azure.ValidateServiceBusTopicName(),
			},

			"resource_group_name": resourceGroupNameSchema(),
		}),

		CustomizeDiff: azure.ServiceBusAuthorizationRuleCustomizeDiff,
	}
}

func resourceArmServiceBusTopicAuthorizationRuleCreateUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).serviceBusTopicsClient
	ctx := meta.(*ArmClient).StopContext
	log.Printf("[INFO] preparing arguments for AzureRM ServiceBus Topic Authorization Rule creation.")

	name := d.Get("name").(string)
	namespaceName := d.Get("namespace_name").(string)
	topicName := d.Get("topic_name").(string)
	resourceGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		// first check if there's one in this subscription requiring import
		resp, err := client.GetAuthorizationRule(ctx, resourceGroup, namespaceName, topicName, name)
		if err != nil {
			if !utils.ResponseWasNotFound(resp.Response) {
				return fmt.Errorf("Error checking for the existence of Service Bus Topic Rule %q (Namespace %q / Resource Group %q): %+v", name, namespaceName, resourceGroup, err)
			}
		}
		if resp.ID != nil {
			return tf.ImportAsExistsError("azurerm_servicebus_subscription_rule", *resp.ID)
		}
	}

	parameters := servicebus.SBAuthorizationRule{
		Name: &name,
		SBAuthorizationRuleProperties: &servicebus.SBAuthorizationRuleProperties{
			Rights: azure.ExpandServiceBusAuthorizationRuleRights(d),
		},
	}

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(tf.TimeoutForCreateUpdate(d)))
	defer cancel()
	if _, err := client.CreateOrUpdateAuthorizationRule(waitCtx, resourceGroup, namespaceName, topicName, name, parameters); err != nil {
		return fmt.Errorf("Error creating/updating ServiceBus Topic Authorization Rule %q (Resource Group %q): %+v", name, resourceGroup, err)
	}

	resp, err := client.GetAuthorizationRule(ctx, resourceGroup, namespaceName, topicName, name)
	if err != nil {
		return err
	}
	if resp.ID == nil {
		return fmt.Errorf("Cannot read ServiceBus Topic Authorization Rule %s (resource group %s) ID", name, resourceGroup)
	}

	d.SetId(*resp.ID)

	return resourceArmServiceBusTopicAuthorizationRuleRead(d, meta)
}

func resourceArmServiceBusTopicAuthorizationRuleRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).serviceBusTopicsClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resGroup := id.ResourceGroup
	namespaceName := id.Path["namespaces"]
	topicName := id.Path["topics"]
	name := id.Path["authorizationRules"]

	resp, err := client.GetAuthorizationRule(ctx, resGroup, namespaceName, topicName, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error making Read request on Azure ServiceBus Topic Authorization Rule %s: %+v", name, err)
	}

	d.Set("name", name)
	d.Set("topic_name", topicName)
	d.Set("namespace_name", namespaceName)
	d.Set("resource_group_name", resGroup)

	if properties := resp.SBAuthorizationRuleProperties; properties != nil {
		listen, send, manage := azure.FlattenServiceBusAuthorizationRuleRights(properties.Rights)
		d.Set("listen", listen)
		d.Set("send", send)
		d.Set("manage", manage)
	}

	keysResp, err := client.ListKeys(ctx, resGroup, namespaceName, topicName, name)
	if err != nil {
		return fmt.Errorf("Error making Read request on Azure ServiceBus Topic Authorization Rule List Keys %s: %+v", name, err)
	}

	d.Set("primary_key", keysResp.PrimaryKey)
	d.Set("primary_connection_string", keysResp.PrimaryConnectionString)
	d.Set("secondary_key", keysResp.SecondaryKey)
	d.Set("secondary_connection_string", keysResp.SecondaryConnectionString)

	return nil
}

func resourceArmServiceBusTopicAuthorizationRuleDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).serviceBusTopicsClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resGroup := id.ResourceGroup
	namespaceName := id.Path["namespaces"]
	topicName := id.Path["topics"]
	name := id.Path["authorizationRules"]

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutDelete))
	defer cancel()
	if _, err = client.DeleteAuthorizationRule(waitCtx, resGroup, namespaceName, topicName, name); err != nil {
		return fmt.Errorf("Error issuing Azure ARM delete request of ServiceBus Topic Authorization Rule %q (Resource Group %q): %+v", name, resGroup, err)
	}

	return nil
}
