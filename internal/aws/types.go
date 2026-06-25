package aws

import "time"

// ─── EC2 ─────────────────────────────────────────────────────────────────────

type Instance struct {
	ID             string
	Name           string
	State          string
	Type           string
	PrivateIP      string
	PublicIP       string
	AZ             string
	VpcID          string
	SubnetID       string
	ImageID        string
	KeyName        string
	LaunchTime     time.Time
	Tags           map[string]string
	SecurityGroups []SecurityGroupRef
}

type SecurityGroupRef struct {
	ID   string
	Name string
}

// ─── Security Groups ─────────────────────────────────────────────────────────

type SecurityGroup struct {
	ID            string
	Name          string
	Description   string
	VpcID         string
	InboundRules  []SGRule
	OutboundRules []SGRule
	Tags          map[string]string
}

type SGRule struct {
	Protocol    string
	PortRange   string
	Source      string
	Description string
}

// ─── Load Balancers ──────────────────────────────────────────────────────────

type LoadBalancer struct {
	Name        string
	ARN         string
	Type        string // application | network | gateway
	Scheme      string // internet-facing | internal
	State       string
	DNSName     string
	VpcID       string
	AZs         []string
	CreatedTime time.Time
}

type Listener struct {
	ARN       string
	LBArn     string
	Port      int32
	Protocol  string
	SSLPolicy string
	Rules     []Rule
}

type Rule struct {
	ARN        string
	Priority   string
	IsDefault  bool
	Conditions []RuleCondition
	Actions    []RuleAction
}

type RuleCondition struct {
	Field  string
	Values []string
}

type RuleAction struct {
	Type           string
	ForwardTargets []ForwardTarget // all TGs when ForwardConfig is present
	TargetGroupARN string          // fallback for simple single-TG forward
	Weight         int32
	RedirectConfig *RedirectConfig
}

type ForwardTarget struct {
	ARN    string
	Weight int32
}

type RedirectConfig struct {
	Protocol   string
	Port       string
	StatusCode string
}

// ─── Target Groups ────────────────────────────────────────────────────────────

type TargetGroup struct {
	Name          string
	ARN           string
	Protocol      string
	Port          int32
	VpcID         string
	TargetType    string // instance | ip | lambda | alb
	HealthCheck   HealthCheckConfig
	LoadBalancers []string
	Targets       []Target
}

type HealthCheckConfig struct {
	Protocol           string
	Path               string
	Port               string
	HealthyThreshold   int32
	UnhealthyThreshold int32
	Interval           int32
	Timeout            int32
}

type Target struct {
	ID           string // instance ID or IP
	Port         int32
	AZ           string
	Health       string // healthy | unhealthy | initial | draining | unused
	HealthDesc   string
	HealthReason string
}

// ─── ECR ─────────────────────────────────────────────────────────────────────

type ECRRepository struct {
	Name       string
	URI        string
	ARN        string
	CreatedAt  time.Time
	Mutability string // MUTABLE | IMMUTABLE
	ScanOnPush bool
}

type ECRImage struct {
	Tags       []string
	Digest     string
	PushedAt   time.Time
	SizeBytes  int64
	ScanStatus string
}

// ─── S3 ──────────────────────────────────────────────────────────────────────

type S3Bucket struct {
	Name      string
	CreatedAt time.Time
	Region    string
}

// ─── EKS ─────────────────────────────────────────────────────────────────────

type EKSCluster struct {
	Name      string
	Status    string
	Version   string
	Endpoint  string
	RoleARN   string
	VpcID     string
	CreatedAt time.Time
	Tags      map[string]string
}

type NodeGroup struct {
	Name          string
	Status        string
	InstanceTypes []string
	DesiredSize   int32
	MinSize       int32
	MaxSize       int32
	AmiType       string
	DiskSize      int32
}

type Addon struct {
	Name    string
	Status  string
	Version string
}
