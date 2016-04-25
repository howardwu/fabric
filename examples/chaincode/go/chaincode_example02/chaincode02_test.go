package chaincode

import (
  "fmt"
  "net"
  "os"
  "strconv"
  "sync"
  "testing"
  "time"

  "github.com/hyperledger/fabric/core/container"
  "github.com/hyperledger/fabric/core/ledger"
  "github.com/hyperledger/fabric/core/util"
  pb "github.com/hyperledger/fabric/protos"
  "github.com/spf13/viper"
  "golang.org/x/net/context"
  "google.golang.org/grpc"
  "google.golang.org/grpc/credentials"
  "google.golang.org/grpc/grpclog"
)

// Build a chaincode.
func getDeploymentSpec(context context.Context, spec *pb.ChaincodeSpec) (*pb.ChaincodeDeploymentSpec, error) {
  fmt.Printf("getting deployment spec for chaincode spec: %v\n", spec)
  codePackageBytes, err := container.GetChaincodePackageBytes(spec)
  if err != nil {
    return nil, err
  }
  chaincodeDeploymentSpec := &pb.ChaincodeDeploymentSpec{ChaincodeSpec: spec, CodePackage: codePackageBytes}
  return chaincodeDeploymentSpec, nil
}

// Deploy a chaincode - i.e., build and initialize.
func deploy(ctx context.Context, spec *pb.ChaincodeSpec) ([]byte, error) {
  // First build and get the deployment spec
  chaincodeDeploymentSpec, err := getDeploymentSpec(ctx, spec)
  if err != nil {
    return nil, err
  }

  tid := chaincodeDeploymentSpec.ChaincodeSpec.ChaincodeID.Name

  // Now create the Transactions message and send to Peer.
  transaction, err := pb.NewChaincodeDeployTransaction(chaincodeDeploymentSpec, tid)
  if err != nil {
    return nil, fmt.Errorf("Error deploying chaincode: %s ", err)
  }

  ledger, err := ledger.GetLedger()
  ledger.BeginTxBatch("1")
  b, err := Execute(ctx, GetChain(DefaultChain), transaction)
  if err != nil {
    return nil, fmt.Errorf("Error deploying chaincode: %s", err)
  }
  ledger.CommitTxBatch("1", []*pb.Transaction{transaction}, nil, nil)

  return b, err
}

// Invoke or query a chaincode.
func invoke(ctx context.Context, spec *pb.ChaincodeSpec, typ pb.Transaction_Type) (string, []byte, error) {
  chaincodeInvocationSpec := &pb.ChaincodeInvocationSpec{ChaincodeSpec: spec}

  // Now create the Transactions message and send to Peer.
  uuid := util.GenerateUUID()

  transaction, err := pb.NewChaincodeExecute(chaincodeInvocationSpec, uuid, typ)
  if err != nil {
    return uuid, nil, fmt.Errorf("Error invoking chaincode: %s ", err)
  }

  var retval []byte
  var execErr error
  if typ == pb.Transaction_CHAINCODE_QUERY {
    retval, execErr = Execute(ctx, GetChain(DefaultChain), transaction)
  } else {
    ledger, _ := ledger.GetLedger()
    ledger.BeginTxBatch("1")
    retval, execErr = Execute(ctx, GetChain(DefaultChain), transaction)
    if err != nil {
      return uuid, nil, fmt.Errorf("Error invoking chaincode: %s ", err)
    }
    ledger.CommitTxBatch("1", []*pb.Transaction{transaction}, nil, nil)
  }

  return uuid, retval, execErr
}

func closeListenerAndSleep(l net.Listener) {
  l.Close()
  time.Sleep(2 * time.Second)
}

// Check the correctness of the final state after transaction execution.
func checkFinalState(uuid string, chaincodeID string) error {
  // Check the state in the ledger
  ledgerObj, ledgerErr := ledger.GetLedger()
  if ledgerErr != nil {
    return fmt.Errorf("Error checking ledger for <%s>: %s", chaincodeID, ledgerErr)
  }

  // Invoke ledger to get state
  var Aval, Bval int
  resbytes, resErr := ledgerObj.GetState(chaincodeID, "a", false)
  if resErr != nil {
    return fmt.Errorf("Error retrieving state from ledger for <%s>: %s", chaincodeID, resErr)
  }
  fmt.Printf("Got string: %s\n", string(resbytes))
  Aval, resErr = strconv.Atoi(string(resbytes))
  if resErr != nil {
    return fmt.Errorf("Error retrieving state from ledger for <%s>: %s", chaincodeID, resErr)
  }
  if Aval != 90 {
    return fmt.Errorf("Incorrect result. Aval is wrong for <%s>", chaincodeID)
  }

  resbytes, resErr = ledgerObj.GetState(chaincodeID, "b", false)
  if resErr != nil {
    return fmt.Errorf("Error retrieving state from ledger for <%s>: %s", chaincodeID, resErr)
  }
  Bval, resErr = strconv.Atoi(string(resbytes))
  if resErr != nil {
    return fmt.Errorf("Error retrieving state from ledger for <%s>: %s", chaincodeID, resErr)
  }
  if Bval != 210 {
    return fmt.Errorf("Incorrect result. Bval is wrong for <%s>", chaincodeID)
  }

  // Success
  fmt.Printf("Aval = %d, Bval = %d\n", Aval, Bval)
  return nil
}

// Invoke chaincode_example02
func invokeExample02Transaction(ctxt context.Context, cID *pb.ChaincodeID, args []string) error {

  f := "init"
  argsDeploy := []string{"a", "100", "b", "200"}
  spec := &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Function: f, Args: argsDeploy}}
  _, err := deploy(ctxt, spec)
  chaincodeID := spec.ChaincodeID.Name
  if err != nil {
    return fmt.Errorf("Error deploying <%s>: %s", chaincodeID, err)
  }

  time.Sleep(time.Second)

  f = "invoke"
  spec = &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Function: f, Args: args}}
  uuid, _, err := invoke(ctxt, spec, pb.Transaction_CHAINCODE_INVOKE)
  if err != nil {
    return fmt.Errorf("Error invoking <%s>: %s", chaincodeID, err)
  }

  err = checkFinalState(uuid, chaincodeID)
  if err != nil {
    return fmt.Errorf("Incorrect final state after transaction for <%s>: %s", chaincodeID, err)
  }

  // Test for delete state
  f = "delete"
  delArgs := []string{"a"}
  spec = &pb.ChaincodeSpec{Type: 1, ChaincodeID: cID, CtorMsg: &pb.ChaincodeInput{Function: f, Args: delArgs}}
  uuid, _, err = invoke(ctxt, spec, pb.Transaction_CHAINCODE_INVOKE)
  if err != nil {
    return fmt.Errorf("Error deleting state in <%s>: %s", chaincodeID, err)
  }

  return nil
}

// Test the invocation of a transaction.
func TestExecuteInvokeTransaction(t *testing.T) {
  var opts []grpc.ServerOption
  if viper.GetBool("peer.tls.enabled") {
    creds, err := credentials.NewServerTLSFromFile(viper.GetString("peer.tls.cert.file"), viper.GetString("peer.tls.key.file"))
    if err != nil {
      grpclog.Fatalf("Failed to generate credentials %v", err)
    }
    opts = []grpc.ServerOption{grpc.Creds(creds)}
  }
  grpcServer := grpc.NewServer(opts...)
  viper.Set("peer.fileSystemPath", "/var/hyperledger/test/tmpdb")

  //use a different address than what we usually use for "peer"
  //we override the peerAddress set in chaincode_support.go
  peerAddress := "0.0.0.0:40303"

  lis, err := net.Listen("tcp", peerAddress)
  if err != nil {
    t.Fail()
    t.Logf("Error starting peer listener %s", err)
    return
  }

  getPeerEndpoint := func() (*pb.PeerEndpoint, error) {
    return &pb.PeerEndpoint{ID: &pb.PeerID{Name: "testpeer"}, Address: peerAddress}, nil
  }

  ccStartupTimeout := time.Duration(chaincodeStartupTimeoutDefault) * time.Millisecond
  pb.RegisterChaincodeSupportServer(grpcServer, NewChaincodeSupport(DefaultChain, getPeerEndpoint, false, ccStartupTimeout, nil))

  go grpcServer.Serve(lis)

  var ctxt = context.Background()

  url := "github.com/hyperledger/fabric/examples/chaincode/go/chaincode_example02"
  chaincodeID := &pb.ChaincodeID{Path: url}

  args := []string{"a", "b", "10"}
  err = invokeExample02Transaction(ctxt, chaincodeID, args)
  if err != nil {
    t.Fail()
    t.Logf("Error invoking transaction: %s", err)
  } else {
    fmt.Printf("Invoke test passed\n")
    t.Logf("Invoke test passed")
  }

  GetChain(DefaultChain).StopChaincode(ctxt, chaincodeID)

  closeListenerAndSleep(lis)
}

func TestMain(m *testing.M) {
  SetupTestConfig()
  viper.Set("ledger.blockchain.deploy-system-chaincode", "false")
  viper.Set("validator.validity-period.verification", "false")
  os.Exit(m.Run())
}