package provider

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/rackn/terraform-provider-drp/client"
)

// This function doesn't really *create* a new machine but, power an already registered
// machine.
func resourceDRPMachineCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceDRPMachineCreate] Launching new drp_machine")
	cc := meta.(*client.Client)

	constraints, err := parseConstraints(d)
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineCreate] Unable to parse constraints.")
		return err
	}

	machineObj, err := cc.AllocateMachine(constraints)
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineCreate] Unable to allocate machine: %v", err)
		return err
	}

	cBootEnv := machineObj.BootEnv

	// Update the machine to request position
	err = cc.UpdateMachine(machineObj, constraints)
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineCreate] Unable to initialize machine: %v", err)
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPMachineCreate] Unable to release machine: %v", err2)
		}
		return err
	}

	if err := cc.MachineDo(machineObj.UUID(), "nextbootpxe", url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceDRPMachineCreate] Unable to mark the machine for pxe next boot: %s\n", machineObj.UUID())
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPMachineCreate] Unable to release machine: %v", err2)
		}
		return err
	}

	// Power on and then cycle, if needed
	powerAction := "poweron"
	if err := cc.MachineDo(machineObj.UUID(), powerAction, url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceDRPMachineCreate] Unable to power cycleup machine: %s\n", machineObj.UUID())
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPMachineCreate] Unable to release machine: %v", err2)
		}
		return err
	}

	machineObj, err = cc.GetMachine(machineObj.UUID())
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineCreate] Unable to release machine: %v", err)
		return err
	}
	if machineObj.BootEnv != cBootEnv {
		powerAction := "powercycle"

		if err := cc.MachineDo(machineObj.UUID(), powerAction, url.Values{}); err != nil {
			log.Printf("[ERROR] [resourceDRPMachineCreate] Unable to power cycleup machine: %s\n", machineObj.UUID())
			if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
				log.Println("[ERROR] [resourceDRPMachineCreate] Unable to release machine: %v", err2)
			}
			return err
		}
	}

	log.Printf("[DEBUG] [resourceDRPMachineCreate] Waiting for machine (%s) to become active\n", machineObj.UUID())
	stateConf := &resource.StateChangeConf{
		Pending:    []string{"9:"},
		Target:     []string{"6:"},
		Refresh:    cc.GetMachineStatus(machineObj.UUID()),
		Timeout:    25 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := stateConf.WaitForState(); err != nil {
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPMachineCreate] Unable to release machine: %v", err2)
		}
		return fmt.Errorf(
			"[ERROR] [resourceDRPMachineCreate] Error waiting for machine (%s) to become deployed: %s",
			machineObj.UUID(), err)
	}

	d.SetId(machineObj.UUID())
	return nil
}

func resourceDRPMachineExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	cc := meta.(*client.Client)
	log.Printf("[DEBUG] Exists machine (%s) information.\n", d.Id())
	return cc.ExistsMachine(d.Id())
}

func resourceDRPMachineRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Reading machine (%s) information.\n", d.Id())
	return nil
}

func resourceDRPMachineUpdate(d *schema.ResourceData, meta interface{}) error {
	cc := meta.(*client.Client)
	log.Printf("[DEBUG] [resourceDRPMachineUpdate] Modifying machine %s\n", d.Id())

	constraints, err := parseConstraints(d)
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineUpdate] Unable to parse constraints.")
		return err
	}

	machineObj, err := cc.GetMachine(d.Id())
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineUpdate] Failed to get machine: %v", err)
		return err
	}

	// Update the machine to request position
	err = cc.UpdateMachine(machineObj, constraints)
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineUpdate] Unable to initialize machine: %v", err)
		return err
	}

	log.Printf("[DEBUG] Done Modifying machine %s", d.Id())
	return nil
}

// This function doesn't really *delete* a drp managed machine but releases (read, turns off) the machine.
func resourceDRPMachineDelete(d *schema.ResourceData, meta interface{}) error {
	cc := meta.(*client.Client)
	log.Printf("[DEBUG] Deleting machine %s\n", d.Id())

	machineObj, err := cc.GetMachine(d.Id())
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineDelete] Failed to get machine: %v", err)
		return err
	}

	retVal := url.Values{}
	if machineObj.Stage != "" {
		retVal["stage"] = []string{"discover"}
	} else {
		retVal["bootenv"] = []string{"sledgehammer"}
	}

	// Update the machine to request position
	err = cc.UpdateMachine(machineObj, retVal)
	if err != nil {
		log.Println("[ERROR] [resourceDRPMachineDelete] Unable to reset machine: %v", err)
		return err
	}

	if err := cc.ReleaseMachine(d.Id()); err != nil {
		return err
	}

	if err := cc.MachineDo(machineObj.UUID(), "nextbootpxe", url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceDRPMachineRelease] Unable to mark the machine for pxe next boot: %s\n", machineObj.UUID())
	}
	if err := cc.MachineDo(machineObj.UUID(), "powercycle", url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceDRPMachineRelease] Unable to power cycle machine: %s\n", machineObj.UUID())
	}

	log.Printf("[DEBUG] [resourceDRPMachineDelete] Machine (%s) released", d.Id())

	d.SetId("")

	return nil
}

var stringParams = []string{
	"name",
	"bootenv",
	"stage",
	"owner",
	"description",
}

func parseConstraints(d *schema.ResourceData) (url.Values, error) {
	log.Println("[DEBUG] [parseConstraints] Parsing any existing DRP constraints")
	retVal := url.Values{}

	for _, s := range stringParams {
		sval, set := d.GetOk(s)
		if set {
			log.Printf("[DEBUG] [parseConstraints] setting %s to %+v", s, sval)
			retVal[s] = strings.Fields(sval.(string))
		}
	}

	udval, set := d.GetOk("userdata")
	if set {
		retVal["userdata"] = []string{udval.(string)}
	}

	retVal["profiles"] = []string{}
	aval, set := d.GetOk("profiles")
	if set {
		for _, p := range aval.([]interface{}) {
			retVal["profiles"] = append(retVal["profiles"], p.(string))
		}
	}

	retVal["parameters"] = []string{}
	pval, set := d.GetOk("parameters")
	if set {
		for _, o := range pval.([]interface{}) {
			v := o.(map[string]interface{})
			name := v["name"]
			value := v["value"].(string)
			retVal["parameters"] = append(retVal["parameters"], fmt.Sprintf("%s=%s", name, value))
		}
	}

	retVal["filters"] = []string{}
	pval, set = d.GetOk("filters")
	if set {
		for _, o := range pval.([]interface{}) {
			v := o.(map[string]interface{})
			name := v["name"]
			value := v["value"].(string)
			retVal["filters"] = append(retVal["filters"], fmt.Sprintf("%s=%s", name, value))
		}
	}

	return retVal, nil
}

func resourceDRPMachine() *schema.Resource {
	log.Println("[DEBUG] [resourceDRPMachine] Initializing data structure")
	return &schema.Resource{
		Create: resourceDRPMachineCreate,
		Read:   resourceDRPMachineRead,
		Update: resourceDRPMachineUpdate,
		Delete: resourceDRPMachineDelete,
		Exists: resourceDRPMachineExists,

		SchemaVersion: 1,

		Schema: map[string]*schema.Schema{
			"bootenv": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"stage": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"owner": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"name": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"description": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"userdata": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"filters": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"value": {
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
			},

			"profiles": {
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},

			"parameters": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"value": {
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
			},
		},
	}
}