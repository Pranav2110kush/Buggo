package cmd

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aztecrabbit/bugscanner-go/pkg/queue_scanner"
	"github.com/spf13/cobra"
)

// scanDirectCmd represents the scanDirect command
var scanDirectCmd = &cobra.Command{
	Use:   "direct",
	Short: "Scan using direct connection",
	Run:   scanDirectRun,
}

var (
	scanDirectFlagFilename string
	scanDirectFlagTimeout  int
	scanDirectFlagOutput   string
)

func init() {
	scanCmd.AddCommand(scanDirectCmd)

	scanDirectCmd.Flags().StringVarP(&scanDirectFlagFilename, "filename", "f", "", "domain list filename")
	scanDirectCmd.Flags().IntVar(&scanDirectFlagTimeout, "timeout", 3, "connect timeout")
	scanDirectCmd.Flags().StringVarP(&scanDirectFlagOutput, "output", "o", "", "output result")

	scanDirectCmd.MarkFlagFilename("filename")
	scanDirectCmd.MarkFlagRequired("filename")
}

type scanDirectRequest struct {
	IP     string
	Domain string
	Server string
}

type scanDirectResponse struct {
	Request    *scanDirectRequest
	StatusCode int
	Server     string
	Location   string
}

var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
	Timeout: 10 * time.Second,
}

func scanDirect(c *queue_scanner.Ctx, p *queue_scanner.QueueScannerScanParams) {
	req := p.Data.(*scanDirectRequest)

	//

	httpReq, err := http.NewRequest("HEAD", fmt.Sprintf("http://%s", req.IP), nil)
	if err != nil {
		return
	}

	httpRes, err := httpClient.Do(httpReq)
	if err != nil {
		return
	}

	hServer := httpRes.Header.Get("Server")
	hRedirect := httpRes.Header.Get("Location")
	if hRedirect != "" {
		hRedirect = fmt.Sprintf(" -> %s", hRedirect)
	}

	s := fmt.Sprintf(
		"%-15s  %-3d  %-16s    %s%s",
		req.IP,
		httpRes.StatusCode,
		hServer,
		req.Domain,
		hRedirect,
	)

	if hServer == req.Server {
		if req.Server == "cloudflare" && httpRes.StatusCode != 403 {
			// Scan failed
		} else {
			s = colorG1.Sprint(s)
			res := &scanDirectResponse{
				Request:    req,
				StatusCode: httpRes.StatusCode,
				Server:     httpRes.Header.Get("Server"),
				Location:   httpRes.Header.Get("Location"),
			}
			c.ScanSuccess(res, nil)
		}
	}

	c.Log(s)
}

func scanDirectRun(cmd *cobra.Command, args []string) {
	domainList := make(map[string]bool)

	domainListFile, err := os.Open(scanDirectFlagFilename)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer domainListFile.Close()

	scanner := bufio.NewScanner(domainListFile)
	for scanner.Scan() {
		domain := scanner.Text()
		domainList[domain] = true
	}

	//

	IPList := make(map[string]string)

	ctx := context.Background()

	for domain := range domainList {
		fmt.Printf("\r\033[2KResolving %s\r", domain)
		ctxTimeout, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		netIPList, err := net.DefaultResolver.LookupIP(ctxTimeout, "ip4", domain)
		if err != nil {
			continue
		}
		for _, ip := range netIPList {
			IPList[ip.String()] = domain
		}
	}

	//

	queueScanner := queue_scanner.NewQueueScanner(scanFlagThreads, scanDirect)
	for ip, domain := range IPList {
		queueScanner.Add(&queue_scanner.QueueScannerScanParams{
			Name: domain,
			Data: &scanDirectRequest{
				IP:     ip,
				Domain: domain,
				Server: "cloudflare",
			},
		})
	}
	queueScanner.Start(func(c *queue_scanner.Ctx) {
		if len(c.ScanSuccessList) == 0 {
			return
		}

		c.Log("")

		ipList := make([]string, 0)

		for _, data := range c.ScanSuccessList {
			res, ok := data.(*scanDirectResponse)
			if !ok {
				continue
			}
			ip := res.Request.IP
			ipList = append(ipList, ip)
			c.Log(colorG1.Sprint(ip))
		}

		if scanDirectFlagOutput != "" {
			err := os.WriteFile(scanDirectFlagOutput, []byte(strings.Join(ipList, "\n")), 0644)
			if err != nil {
				fmt.Println(err.Error())
				os.Exit(1)
			}
		}
	})
}