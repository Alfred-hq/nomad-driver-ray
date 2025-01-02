package templates

// Rename dummy_template to DummyTemplate to export it
// const RayActorTemplate = `
// import ray
// import time
// import sys
// import os
// import importlib

// @ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
// class {{.Actor}}:
//     def {{.Runner}}(self):
//         try:
//             directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")

//             # Get the file name without the extension
//             file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

//             sys.path.append(directory_path)

//             # Dynamically import the module
//             pipeline_module = importlib.import_module(file_name)

//             # Execute the pipeline function
//             getattr(pipeline_module, \"{{.PipelineRunner}}\")()
//         except Exception as e:
//             print(e)
//         finally:
//             ray.actor.exit_actor()

// # Initialize connection to the Ray head node on the default port.
// ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

// pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2, num_cpus={{.NumCPUs}}).remote()
// `

const RemoteRunnerTemplate = `
import ray
import os
import sys
import importlib

ray.init(address="auto", namespace="{{.Namespace}}")

def main():
    try:
        # Dummy job: loop 10,000 times, log and sleep
        for i in range(10000):
            print(f"Loop iteration {i+1} / 10000")
            time.sleep(0.1)  # Sleep for 0.1 seconds between iterations
    except Exception as e:
        print(f"Error running dummy job: {e}")

if __name__ == "__main__":
    main()

`
