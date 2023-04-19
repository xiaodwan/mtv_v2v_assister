package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/vmware/govmomi/session/cache"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
)

// global options
var (
	_name = flag.String("name", "", "The specific name")
)

var motypes []string

var (
	dss      []mo.Datastore
	networks []mo.Network
	vms      []mo.VirtualMachine
)

var urlDescription = fmt.Sprintf("ESX or vCenter URL [%s]", envURL)
var urlFlag = flag.String("url", getEnvString(envURL, ""), urlDescription)

var insecureDescription = fmt.Sprintf("Don't verify the server's certificate chain [%s]", envInsecure)
var insecureFlag = flag.Bool("insecure", getEnvBool(envInsecure, false), insecureDescription)

const (
	envURL      = "GOVMOMI_URL"
	envUserName = "GOVMOMI_USERNAME"
	envPassword = "GOVMOMI_PASSWORD"
	envInsecure = "GOVMOMI_INSECURE"
)

// getEnvString returns string from environment variable.
func getEnvString(v string, def string) string {
	r := os.Getenv(v)
	if r == "" {
		return def
	}

	return r
}

// getEnvBool returns boolean from environment variable.
func getEnvBool(v string, def bool) bool {
	r := os.Getenv(v)
	if r == "" {
		return def
	}

	switch strings.ToLower(r[0:1]) {
	case "t", "y", "1":
		return true
	}

	return false
}

func processOverride(u *url.URL) {
	envUsername := os.Getenv(envUserName)
	envPassword := os.Getenv(envPassword)

	// Override username if provided
	if envUsername != "" {
		var password string
		var ok bool

		if u.User != nil {
			password, ok = u.User.Password()
		}

		if ok {
			u.User = url.UserPassword(envUsername, password)
		} else {
			u.User = url.User(envUsername)
		}
	}

	// Override password if provided
	if envPassword != "" {
		var username string

		if u.User != nil {
			username = u.User.Username()
		}

		u.User = url.UserPassword(username, envPassword)
	}
}

func NewClient(ctx context.Context) (*vim25.Client, error) {
	u, err := soap.ParseURL(*urlFlag)
	if err != nil {
		return nil, err
	}

	processOverride(u)

	s := &cache.Session{
		URL:      u,
		Insecure: *insecureFlag,
	}

	c := new(vim25.Client)
	err = s.Login(ctx, c, nil)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func run(ctx context.Context, c *vim25.Client) error {
	m := view.NewManager(c)
	v, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, motypes, true)
	if err != nil {
		return err
	}

	defer v.Destroy(ctx)

	for _, o := range motypes {
		switch o {
		case "VirtualMachine":
			err = v.Retrieve(ctx, []string{"VirtualMachine"}, nil, &vms)
		case "Datastore":
			err = v.Retrieve(ctx, []string{"Datastore"}, nil, &dss)
		case "Network":
			err = v.Retrieve(ctx, []string{"Network"}, nil, &networks)
		default:
			log.Fatalf("Unknow Subcommand: %s", o)
		}
	}

	if err != nil {
		return err
	}
	return nil
}

func isExpected(v string, s string) bool {
	if v == s || s == "" {
		return true
	}
	return false
}

func tabwriter_print(motype []string, names string) {
	tw := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Name:\tType:\t\n")

	for _, v := range motype {
		switch v {
		case "VirtualMachine":
			for _, vm := range vms {
				if isExpected(vm.Summary.Config.Name, names) {
					fmt.Fprintf(tw, "%s\t", vm.Summary.Config.Name)
					fmt.Fprintf(tw, "%s\t", vm.Summary.Vm)
					fmt.Fprintf(tw, "\n")
				}
			}
		case "Network":
			for _, net := range networks {
				if isExpected(net.Name, names) {
					fmt.Fprintf(tw, "%s\t", net.Name)
					fmt.Fprintf(tw, "%s\t", net.Summary.GetNetworkSummary().Network)
					fmt.Fprintf(tw, "\n")
				}
			}
		case "Datastore":
			for _, ds := range dss {
				if isExpected(ds.Summary.Name, names) {
					fmt.Fprintf(tw, "%s\t", ds.Summary.Name)
					fmt.Fprintf(tw, "%s\t", ds.Summary.Datastore)
					fmt.Fprintf(tw, "\n")
				}
			}
		}
	}
	_ = tw.Flush()

}

func registerGlobalFlags(fset *flag.FlagSet) {
	flag.VisitAll(func(f *flag.Flag) {
		fset.Var(f.Value, f.Name, f.Usage)
	})
}

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		log.Fatal("Please specify a subcommand.")
	}
	cmd, args := args[0], args[1:]

	switch cmd {
	case "vm":
		motypes = append(motypes, "VirtualMachine")
		vmflag := flag.NewFlagSet("vm", flag.ExitOnError)
		registerGlobalFlags(vmflag)
		vmflag.Parse(args)
		args = vmflag.Args()
	case "datastore":
		motypes = append(motypes, "Datastore")
		datastoreflag := flag.NewFlagSet("datastore", flag.ExitOnError)
		registerGlobalFlags(datastoreflag)
		datastoreflag.Parse(args)
		args = datastoreflag.Args()
	case "network":
		motypes = append(motypes, "Network")
		networkflag := flag.NewFlagSet("network", flag.ExitOnError)
		registerGlobalFlags(networkflag)
		networkflag.Parse(args)
		args = networkflag.Args()
	case "all":
		motypes = append(motypes, []string{"Datastore", "VirtualMachine", "Network"}...)
		allflag := flag.NewFlagSet("all", flag.ExitOnError)
		registerGlobalFlags(allflag)
		allflag.Parse(args)
		args = allflag.Args()
	default:
		log.Fatalf("Unrecognized command %q. "+
			"Command must be one of: vm, datastore, network", cmd)
	}

	if *urlFlag == "" {
		log.Fatal("url must be set")
	}

	var c *vim25.Client
	var err error
	ctx := context.Background()
	c, err = NewClient(ctx)
	if err != nil {
		log.Fatal("Create client error: ", err)
	}
	run(ctx, c)
	tabwriter_print(motypes, *_name)

}
