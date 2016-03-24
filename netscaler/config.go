package netscaler

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

type NetscalerService struct {
	Name        string `json:"name"`
	Ip          string `json:"ip"`
	ServiceType string `json:"serviceType"`
	Port        int    `json:"port"`
}

type NetscalerLB struct {
	Name        string `json:"name"`
	Ipv46       string `json:"ipv46"`
	ServiceType string `json:"serviceType"`
	Port        int    `json:"port"`
}

type NetscalerLBServiceBinding struct {
	Name        string `json:"name"`
	ServiceName string `json:"serviceName"`
}

type NetscalerCsAction struct {
	Name            string `json:"name"`
	TargetLBVserver string `json:"targetLBVserver"`
}

type NetscalerCsPolicy struct {
	PolicyName string `json:"policyName"`
	Rule       string `json:"rule"`
	Action     string `json:"action"`
}

type NetscalerCsPolicyBinding struct {
	Name       string `json:"name"`
	PolicyName string `json:"policyName"`
	Priority   int    `json:"priority"`
	Bindpoint  string `json:"bindpoint"`
}

type NetscalerCsVserver struct {
	Name        string `json:"name"`
	ServiceType string `json:"serviceType"`
	Ipv46       string `json:"ipv46"`
	Port        int    `json:"port"`
}

func ConfigureContentVServer(csvserverName string, domainName string, path string, serviceIp string, serviceName string, servicePort int) {
	lbName := strings.Replace(domainName, ".", "_", -1) + "_lb"

	//create a Netscaler Service that represents the Kubernetes service
	nsService := &struct {
		Service NetscalerService `json:"service"`
	}{Service: NetscalerService{Name: serviceName, Ip: serviceIp, ServiceType: "HTTP", Port: servicePort}}
	resourceJson, err := json.Marshal(nsService)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to marshal service %s err=", serviceName, err))
		return
	}
	log.Println(string(resourceJson))

	resourceType := "service"

	body, err := createResource(resourceType, resourceJson)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to create service %s err=%s", serviceName, err))
		return
	}
	_ = body

	//create a Netscaler "lbvserver" to front the service
	nsLB := &struct {
		Lbvserver NetscalerLB `json:"lbvserver"`
	}{Lbvserver: NetscalerLB{Name: lbName, Ipv46: "0.0.0.0", ServiceType: "HTTP", Port: 0}}
	resourceJson, err = json.Marshal(nsLB)

	resourceType = "lbvserver"

	body, err = createResource(resourceType, resourceJson)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to create lb %s, err=%s", lbName, err))
		//TODO roll back
		return
	}

	//bind the lb to the service
	nsLbSvcBinding := &struct {
		Lbvserver_service_binding NetscalerLBServiceBinding `json:"lbvserver_service_binding"`
	}{Lbvserver_service_binding: NetscalerLBServiceBinding{Name: lbName, ServiceName: serviceName}}
	resourceJson, err = json.Marshal(nsLbSvcBinding)
	resourceType = "lbvserver_service_binding"

	body, err = createResource(resourceType, resourceJson)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to bind lb %s to service %s, err=%s", lbName, serviceName, err))
		//TODO roll back
		return
	}

	//create a content switch action to switch to the lb
	actionName := "switch_to_lb_" + lbName
	nsCsAction := &struct {
		Csaction NetscalerCsAction `json:"csaction"`
	}{Csaction: NetscalerCsAction{Name: actionName, TargetLBVserver: lbName}}
	resourceJson, err = json.Marshal(nsCsAction)
	resourceType = "csaction"

	body, err = createResource(resourceType, resourceJson)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to create Content Switching Action %s to LB %s err=%s", actionName, lbName, err))
		//TODO roll back
		return
	}

	//create a content switch policy to use the action
	policyName := "switch_to_lb_" + lbName + "_policy"
	var rule string
	if path != "" {
		rule = fmt.Sprintf("HTTP.REQ.HOSTNAME.EQ(\"%s\") && HTTP.REQ.URL.PATH.EQ(\"%s\")", domainName, path)
	} else {
		rule = fmt.Sprintf("HTTP.REQ.HOSTNAME.EQ(\"%s\")", domainName)
	}
	nsCsPolicy := &struct {
		Cspolicy NetscalerCsPolicy `json:"cspolicy"`
	}{Cspolicy: NetscalerCsPolicy{PolicyName: policyName, Rule: rule, Action: actionName}}
	resourceJson, err = json.Marshal(nsCsPolicy)
	resourceType = "cspolicy"

	body, err = createResource(resourceType, resourceJson)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to create Content Switching Policy %s, err=%s", policyName, err))
		//TODO roll back
		return
	}

	//bind the content switch policy to the content switching vserver
	nsCsPolicyBinding := &struct {
		Csvserver_cspolicy_binding NetscalerCsPolicyBinding `json:"csvserver_cspolicy_binding"`
	}{Csvserver_cspolicy_binding: NetscalerCsPolicyBinding{Name: csvserverName, PolicyName: policyName, Priority: 10, Bindpoint: "REQUEST"}}
	resourceJson, err = json.Marshal(nsCsPolicyBinding)
	resourceType = "csvserver_cspolicy_binding"

	body, err = createResource(resourceType, resourceJson)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to bind Content Switching Policy %s to Content Switching VServer %s, err=%s", policyName, csvserverName, err))
		return
	}

}

func CreateContentVServer(csvserverName string, vserverIp string, vserverPort int) {
	contentServer := &struct {
		Csvserver NetscalerCsVserver `json:"csvserver"`
	}{Csvserver: NetscalerCsVserver{Name: csvserverName, Ipv46: vserverIp, ServiceType: "HTTP", Port: vserverPort}}
	resourceJson, err := json.Marshal(contentServer)
	resourceType := "csvserver"

	body, err := createResource(resourceType, resourceJson)
	_ = body
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to create Content Switching Vserver %s, err=%s", csvserverName, err))
		return
	}
}

func UnconfigureContentVServer(csvserverName string, domainName string, path string, serviceIp string, serviceName string, servicePort int) {
	lbName := strings.Replace(domainName, ".", "_", -1) + "_lb"
	actionName := "switch_to_lb_" + lbName
	policyName := "switch_to_lb_" + lbName + "_policy"

	//unbind the content switch policy from the content switching vserver
	resourceType := "csvserver_cspolicy_binding"

	body, err := unbindResource(resourceType, csvserverName, "policyName", policyName)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to unbind Content Switching Policy %s fromo Content Switching VServer %s, err=%s", policyName, csvserverName, err))
		return
	}

	//delete the content switch policy that uses the action
	resourceType = "cspolicy"

	body, err = deleteResource(resourceType, policyName)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to delete Content Switching Policy %s, err=%s", policyName, err))
		return
	}

	//delete content switch action that switches to the lb
	resourceType = "csaction"

	body, err = deleteResource(resourceType, actionName)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to delete Content Switching Action %s for LB %s err=%s", actionName, lbName, err))
		return
	}

	//unbind the service from the LB
	resourceType = "lbvserver_service_binding"

	body, err = unbindResource(resourceType, lbName, "servicename", serviceName)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to unbind svc %s from lb %s, err=%s", serviceName, lbName, err))
		return
	}

	//delete  "lbvserver" that fronts the service
	resourceType = "lbvserver"

	body, err = deleteResource(resourceType, lbName)
	if err != nil {
		log.Println(fmt.Sprintf("Failed to delete lb %s, err=%s", lbName, err))
	}

	//Delete the Netscaler Service
	resourceType = "service"

	body, err = deleteResource(resourceType, serviceName)
	if err != nil {
		log.Println(fmt.Sprintf("Failed to delete %s err=%s", serviceName, err))
	}
	_ = body

}