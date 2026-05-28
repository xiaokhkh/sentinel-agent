# Reproduce the README demo GIF with charmbracelet/vhs:
#   brew install vhs
#   minikube start --driver=docker --image-mirror-country=cn   # (unset *_PROXY first)
#   kubectl apply -f docs/demo-k8s.yaml                          # sample workload (healthy + broken pod)
#   guard serve &                                                # warm the local model
#   vhs docs/demo.tape                                           # -> docs/assets/demo.gif
Output "docs/assets/demo.gif"

Set Shell "bash"
Set FontSize 15
Set Width 1200
Set Height 780
Set Padding 18
Set Theme "Dracula"
Set TypingSpeed 45ms

Hide
Type "export PATH=$PWD/bin:$PATH && clear"
Enter
Sleep 1s
Show

Type "# 1) on-device model + engine status"
Enter
Type "guard model"
Enter
Sleep 2.5s

Type "# 2) natural language -> local model -> Policy Guard -> live minikube"
Enter
Type "guard run --mode readonly 'show me all pods in the default namespace'"
Enter
Sleep 9s

Type "# 3) the safety fence refuses destructive ops"
Enter
Type "guard policy check 'kubectl delete pods --all -n default'"
Enter
Sleep 3.5s
