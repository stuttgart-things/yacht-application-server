/*
Copyright © 2023 Patrick Hermann patrick.hermann@sva.de
*/

package server

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"text/template"
	"time"

	revisionrun "github.com/stuttgart-things/stageTime-server/revisionrun"
	sthingsBase "github.com/stuttgart-things/sthingsBase"
)

var (
	pipelineNamespace = os.Getenv("PIPELINE_WORKSPACE")
)

type PipelineRun struct {
	Name                string
	RevisionRunAuthor   string
	RevisionRunRepoName string
	RevisionRunRepoUrl  string
	RevisionRunCommitId string
	RevisionRunCreation string
	Namespace           string
	PipelineRef         string
	ServiceAccount      string
	Timeout             string
	Params              map[string]string
	ListParams          map[string][]string
	Workspaces          []Workspace
	NamePrefix          string
	NameSuffix          string
	Stage               string
}

type Workspace struct {
	Name                   string
	WorkspaceKind          string
	WorkspaceRef           string
	WorkspaceKindShortName string
}

type RevisionRun struct {
	Name        string
	Namespace   string
	Repository  string
	Stages      []string
	PipelinRuns []string
}

const PipelineRunTemplate = `
apiVersion: tekton.dev/v1beta1
kind: PipelineRun
metadata:
  name: "{{ .NamePrefix }}-{{ .Stage }}-{{ .Name }}-{{ .NameSuffix }}"
  namespace: {{ .Namespace }}
  labels:
    argocd.argoproj.io/instance: tekton-runs
    stagetime/commit: "{{ .RevisionRunCommitId }}"
    stagetime/repo: {{ .RevisionRunRepoName }}
    stagetime/author: {{ .RevisionRunAuthor }}
    stagetime/stage: "{{ .Stage }}"
    tekton.dev/pipeline: {{ .PipelineRef }}
spec:
  serviceAccountName: {{ .ServiceAccount }}
  timeout: {{ .Timeout }}
  pipelineRef:
    name: {{ .PipelineRef }}
  params:{{ range $name, $value := .Params }}
  - name: {{ $name }}
    value: {{ $value }}{{ end }}{{ if .ListParams }}{{ range $name, $values := .ListParams }}
  - name: {{ $name }}
    value: {{ range $values }}
      - {{ . }}{{ end }}{{ end }}{{ end }}
  workspaces:{{ range .Workspaces }}
  - name: {{ .Name }}
    {{ .WorkspaceKind }}:
      {{ .WorkspaceKindShortName }}: {{ .WorkspaceRef }}{{ end }}
`

const RevisionRunTemplate = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: revisionrun-{{ .RevisionRunCommitId }}
  namespace: {{ .Namespace }}
data:
  revisionRun: |
    repository: {{ .Repository }}
    revision: {{ .RevisionRunCommitId }}
    stages: {{ range .Stages }}
	  - {{ . }}{{ end }}
`

type VariableDelimiter struct {
	begin        string `mapstructure:"begin"`
	end          string `mapstructure:"end"`
	regexPattern string `mapstructure:"regex-pattern"`
}

var Patterns = map[string]VariableDelimiter{
	"curly":  VariableDelimiter{"{{", "}}", `\{\{(.*?)\}\}`},
	"square": VariableDelimiter{"[[", "]]", `\[\[(.*?)\]\]`},
}

func RenderPipelineRuns(gRPCRequest *revisionrun.CreateRevisionRunRequest) (renderedPipelineruns map[int][]string, allStages []string) {

	// GET CURRENT TIME
	dt := time.Now()

	// INIT PR MAP
	renderedPipelineruns = make(map[int][]string)

	// LOOP OVER PR MAP
	for _, pipelinerun := range gRPCRequest.Pipelineruns {

		allStages = append(allStages, fmt.Sprintf("%v", pipelinerun.Stage))

		listPipelineParams := make(map[string][]string)
		pipelineParams := make(map[string]string)
		var pipelineWorkspaces []Workspace

		// fmt.Println(pipelinerun.Name)
		// fmt.Println(pipelinerun.Stage)

		paramValues := strings.Split(pipelinerun.Params, ",")
		for _, v := range paramValues {
			values := strings.Split(v, "=")
			pipelineParams[strings.TrimSpace(values[0])] = strings.TrimSpace(values[1])
			// fmt.Println(i)
			// fmt.Println(strings.TrimSpace(values[0]))
			// fmt.Println(strings.TrimSpace(values[1]))
		}

		for _, v := range strings.Split(pipelinerun.Listparams, ",") {

			keyValues := strings.Split(v, "=")
			var values []string

			for _, v := range strings.Split(strings.TrimSpace(keyValues[1]), ";") {
				values = append(values, v)
				fmt.Println(v)
			}
			listPipelineParams[strings.TrimSpace(keyValues[0])] = values
		}

		workspaces := strings.Split(pipelinerun.Workspaces, ",")

		for _, v := range workspaces {
			values := strings.Split(v, "=")
			workspaces := strings.Split(values[1], ";")
			pipelineWorkspaces = append(pipelineWorkspaces, Workspace{strings.TrimSpace(values[0]), strings.TrimSpace(workspaces[0]), strings.TrimSpace(workspaces[1]), strings.TrimSpace(workspaces[2])})
		}

		// fmt.Println(pipelineWorkspaces)

		pr := PipelineRun{
			Name:                pipelinerun.Name,
			RevisionRunAuthor:   gRPCRequest.Author,
			RevisionRunCreation: gRPCRequest.PushedAt,
			RevisionRunCommitId: gRPCRequest.CommitId,
			RevisionRunRepoUrl:  gRPCRequest.RepoUrl,
			RevisionRunRepoName: gRPCRequest.RepoName,
			Namespace:           pipelineNamespace,
			PipelineRef:         pipelinerun.Name,
			ServiceAccount:      "default",
			Timeout:             "1h",
			Params:              pipelineParams,
			ListParams:          listPipelineParams,
			Stage:               fmt.Sprintf("%v", pipelinerun.Stage),
			NamePrefix:          "st",
			NameSuffix:          dt.Format("020405") + gRPCRequest.CommitId[0:4],
			Workspaces:          pipelineWorkspaces,
		}

		// RENDERING
		var buf bytes.Buffer
		tmpl, err := template.New("pipelinerun").Parse(PipelineRunTemplate)
		if err != nil {
			panic(err)
		}
		err = tmpl.Execute(&buf, pr)
		if err != nil {
			log.Fatalf("execution: %s", err)
		}

		// TEST-OUTPUT
		// fmt.Println(buf.String())

		// ADD RENDERED PRS TO REVISIONRUN
		renderedPipelineruns[int(pipelinerun.Stage)] = append(renderedPipelineruns[int(pipelinerun.Stage)], buf.String())

	}

	allStages = sthingsBase.UniqueSlice(allStages)
	return
}

func RenderOutputData(template, delimiter string, templateKeyValues map[string]string) {

	// convert string to interface map
	templateValueData := make(map[string]interface{})
	for k, v := range templateKeyValues {
		templateValueData[k] = v
	}

	// render template
	renderedTemplate, err := sthingsBase.RenderTemplateInline(template, "missingkey=zero", Patterns[delimiter].begin, Patterns[delimiter].end, templateValueData)

	if err != nil {
		log.Fatal(err)
	}

	renderedData := strings.ReplaceAll(string(renderedTemplate), "&#34;", " ")

	fmt.Println(renderedData)

}
