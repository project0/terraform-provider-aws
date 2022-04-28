package emrcontainers

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/emrcontainers"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
)

func ResourceVirtualCluster() *schema.Resource {
	return &schema.Resource{
		Create: resourceVirtualClusterCreate,
		Read:   resourceVirtualClusterRead,
		Delete: resourceVirtualClusterDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"container_provider": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Required: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},
						// According to https://docs.aws.amazon.com/emr-on-eks/latest/APIReference/API_ContainerProvider.html
						// The info and the eks_info are optional but the API raises ValidationException without the fields
						"info": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Required: true,
							ForceNew: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"eks_info": {
										Type:     schema.TypeList,
										MaxItems: 1,
										Required: true,
										ForceNew: true,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"namespace": {
													Type:     schema.TypeString,
													Optional: true,
													ForceNew: true,
												},
											},
										},
									},
								},
							},
						},
						"type": {
							Type:         schema.TypeString,
							Required:     true,
							ForceNew:     true,
							ValidateFunc: validation.StringInSlice(emrcontainers.ContainerProviderType_Values(), false),
						},
					},
				},
			},
			"created_at": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringMatch(regexp.MustCompile(`[.\-_/#A-Za-z0-9]+`), ""),
			},
			"state": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceVirtualClusterCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EMRContainersConn

	input := emrcontainers.CreateVirtualClusterInput{
		ContainerProvider: expandEMRContainersContainerProvider(d.Get("container_provider").([]interface{})),
		Name:              aws.String(d.Get("name").(string)),
	}

	log.Printf("[INFO] Creating EMR containers virtual cluster: %s", input)
	out, err := conn.CreateVirtualCluster(&input)
	if err != nil {
		return fmt.Errorf("error creating EMR containers virtual cluster: %w", err)
	}

	d.SetId(aws.StringValue(out.Id))

	if _, err := waitVirtualClusterCreated(conn, d.Id()); err != nil {
		return fmt.Errorf("error waiting for EMR containers virtual cluster (%s) creation: %w", d.Id(), err)
	}

	return resourceVirtualClusterRead(d, meta)
}

func resourceVirtualClusterRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EMRContainersConn

	vc, err := findVirtualClusterById(conn, d.Id())

	if err != nil {
		if tfawserr.ErrMessageContains(err, emrcontainers.ErrCodeResourceNotFoundException, "") && !d.IsNewResource() {
			log.Printf("[WARN] EMR containers virtual cluster (%s) not found, removing from state", d.Id())
			d.SetId("")
			return nil
		}

		return fmt.Errorf("error reading EMR containers virtual cluster (%s): %w", d.Id(), err)
	}

	if vc == nil {
		log.Printf("[WARN] EMR containers virtual cluster (%s) not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}

	d.Set("arn", vc.Arn)
	if err := d.Set("container_provider", flattenEMRContainersContainerProvider(vc.ContainerProvider)); err != nil {
		return fmt.Errorf("error reading EMR containers virtual cluster (%s): %w", d.Id(), err)
	}
	d.Set("created_at", aws.TimeValue(vc.CreatedAt).String())
	d.Set("name", vc.Name)
	d.Set("state", vc.State)

	return nil
}

func resourceVirtualClusterDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EMRContainersConn

	log.Printf("[INFO] EMR containers virtual cluster: %s", d.Id())
	_, err := conn.DeleteVirtualCluster(&emrcontainers.DeleteVirtualClusterInput{
		Id: aws.String(d.Id()),
	})
	if err != nil {
		if tfawserr.ErrMessageContains(err, emrcontainers.ErrCodeResourceNotFoundException, "") {
			return nil
		}

		return fmt.Errorf("error deleting EMR containers virtual cluster (%s): %w", d.Id(), err)
	}

	_, err = waitVirtualClusterDeleted(conn, d.Id())

	if err != nil {
		return fmt.Errorf("error waiting for EMR containers virtual cluster (%s) deletion: %w", d.Id(), err)
	}

	return nil
}

func expandEMRContainersContainerProvider(l []interface{}) *emrcontainers.ContainerProvider {
	if len(l) == 0 || l[0] == nil {
		return nil
	}

	m := l[0].(map[string]interface{})

	input := emrcontainers.ContainerProvider{
		Id:   aws.String(m["id"].(string)),
		Type: aws.String(m["type"].(string)),
	}

	if v, ok := m["info"]; ok {
		input.Info = expandEMRContainersContainerInfo(v.([]interface{}))
	}

	return &input
}

func expandEMRContainersContainerInfo(l []interface{}) *emrcontainers.ContainerInfo {
	if len(l) == 0 || l[0] == nil {
		return nil
	}

	m := l[0].(map[string]interface{})

	input := emrcontainers.ContainerInfo{}

	if v, ok := m["eks_info"]; ok {
		input.EksInfo = expandEMRContainersEksInfo(v.([]interface{}))
	}

	return &input
}

func expandEMRContainersEksInfo(l []interface{}) *emrcontainers.EksInfo {
	if len(l) == 0 || l[0] == nil {
		return nil
	}

	m := l[0].(map[string]interface{})

	input := emrcontainers.EksInfo{}

	if v, ok := m["namespace"]; ok {
		input.Namespace = aws.String(v.(string))
	}

	return &input
}

func flattenEMRContainersContainerProvider(cp *emrcontainers.ContainerProvider) []interface{} {
	if cp == nil {
		return []interface{}{}
	}

	m := map[string]interface{}{}

	m["id"] = cp.Id
	m["type"] = cp.Type

	if cp.Info != nil {
		m["info"] = flattenEMRContainersContainerInfo(cp.Info)
	}

	return []interface{}{m}
}

func flattenEMRContainersContainerInfo(ci *emrcontainers.ContainerInfo) []interface{} {
	if ci == nil {
		return []interface{}{}
	}

	m := map[string]interface{}{}

	if ci.EksInfo != nil {
		m["eks_info"] = flattenEMRContainersEksInfo(ci.EksInfo)
	}

	return []interface{}{m}
}

func flattenEMRContainersEksInfo(ei *emrcontainers.EksInfo) []interface{} {
	if ei == nil {
		return []interface{}{}
	}

	m := map[string]interface{}{}

	if ei.Namespace != nil {
		m["namespace"] = ei.Namespace
	}

	return []interface{}{m}
}

// findVirtualClusterById returns the EMR containers virtual cluster corresponding to the specified Id.
// Returns nil if no environment is found.
func findVirtualClusterById(conn *emrcontainers.EMRContainers, id string) (*emrcontainers.VirtualCluster, error) {
	input := &emrcontainers.DescribeVirtualClusterInput{
		Id: aws.String(id),
	}

	output, err := conn.DescribeVirtualCluster(input)
	if err != nil {
		return nil, err
	}

	if output == nil {
		return nil, nil
	}

	return output.VirtualCluster, nil
}

const (
	statusVirtualClusterNotFound = "NotFound"
	statusVirtualClusterUnknown  = "Unknown"
)

// statusVirtualCluster fetches the virtual cluster and its status
func statusVirtualCluster(conn *emrcontainers.EMRContainers, id string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		vc, err := findVirtualClusterById(conn, id)

		if tfawserr.ErrCodeEquals(err, emrcontainers.ErrCodeResourceNotFoundException) {
			return nil, statusVirtualClusterNotFound, nil
		}

		if err != nil {
			return nil, statusVirtualClusterUnknown, err
		}

		if vc == nil {
			return nil, statusVirtualClusterNotFound, nil
		}

		return vc, aws.StringValue(vc.State), nil
	}
}

const (
	// Maximum amount of time to wait for a virtual cluster creation
	VirtualClusterCreatedTimeout = 90 * time.Minute
	// Amount of delay to check a virtual cluster
	VirtualClusterCreatedDelay = 1 * time.Minute

	// Maximum amount of time to wait for a virtual cluster deletion
	VirtualClusterDeletedTimeout = 90 * time.Minute
	// Amount of delay to check a virtual cluster status
	VirtualClusterDeletedDelay = 1 * time.Minute
)

// waitVirtualClusterCreated waits for a virtual cluster to return running
func waitVirtualClusterCreated(conn *emrcontainers.EMRContainers, id string) (*emrcontainers.VirtualCluster, error) {
	stateConf := &resource.StateChangeConf{
		Pending: []string{},
		Target:  []string{emrcontainers.VirtualClusterStateRunning},
		Refresh: statusVirtualCluster(conn, id),
		Timeout: VirtualClusterCreatedTimeout,
		Delay:   VirtualClusterCreatedDelay,
	}

	outputRaw, err := stateConf.WaitForState()

	if v, ok := outputRaw.(*emrcontainers.VirtualCluster); ok {
		return v, err
	}

	return nil, err
}

// waitVirtualClusterDeleted waits for a virtual cluster to be deleted
func waitVirtualClusterDeleted(conn *emrcontainers.EMRContainers, id string) (*emrcontainers.VirtualCluster, error) {
	stateConf := &resource.StateChangeConf{
		Pending: []string{emrcontainers.VirtualClusterStateTerminating},
		Target:  []string{emrcontainers.VirtualClusterStateTerminated},
		Refresh: statusVirtualCluster(conn, id),
		Timeout: VirtualClusterDeletedTimeout,
		Delay:   VirtualClusterDeletedDelay,
	}

	outputRaw, err := stateConf.WaitForState()

	if v, ok := outputRaw.(*emrcontainers.VirtualCluster); ok {
		return v, err
	}

	return nil, err
}
