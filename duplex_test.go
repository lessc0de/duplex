package duplex

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

type Args struct {
	A, B int
}

type Reply struct {
	C int
}

type Arith int

// Some of Arith's methods have value args, some have pointer args. That's deliberate.

func (t *Arith) Add(args Args, reply *Reply) error {
	reply.C = args.A + args.B
	return nil
}

func (t *Arith) Mul(args *Args, reply *Reply) error {
	reply.C = args.A * args.B
	return nil
}

func (t *Arith) Div(args Args, reply *Reply) error {
	if args.B == 0 {
		return errors.New("divide by zero")
	}
	reply.C = args.A / args.B
	return nil
}

func (t *Arith) String(args *Args, reply *string) error {
	*reply = fmt.Sprintf("%d+%d=%d", args.A, args.B, args.A+args.B)
	return nil
}

func (t *Arith) Scan(args string, reply *Reply) (err error) {
	_, err = fmt.Sscan(args, &reply.C)
	return
}

func (t *Arith) Error(args *Args, reply *Reply) error {
	panic("ERROR")
}

func (t *Arith) TakesContext(context *string, args string, reply *string) error {
	return nil
}

func TestSimpleCall(t *testing.T) {
	client := NewPeer()
	if err := client.Bind("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := NewPeer()
	if err := server.Connect("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	server.Register(new(Arith))
	go server.Serve()

	// Synchronous calls
	args := &Args{7, 8}
	reply := new(Reply)
	err := client.Call("Arith.Add", args, reply)
	if err != nil {
		t.Errorf("Add: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A+args.B {
		t.Errorf("Add: expected %d got %d", reply.C, args.A+args.B)
	}
}

type StreamingArgs struct {
	A     int
	Count int
	// next two values have to be between 0 and Count-2 to trigger anything
	ErrorAt int // will trigger an error at the given spot,
}

type StreamingReply struct {
	C     int
	Index int
}

type StreamingArith int

func (t *StreamingArith) Thrive(args StreamingArgs, stream SendStream) error {
	for i := 0; i < args.Count; i++ {
		if i == args.ErrorAt {
			return errors.New("Triggered error in middle")
		}
		err := stream.Send(&StreamingReply{C: args.A, Index: i})
		if err != nil {
			return nil
		}
	}

	return nil
}

func (t *StreamingArith) Sum(channel *Channel) error {
	args := new(StreamingArgs)
	sum := 0
	for {
		err := channel.Receive(args)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		sum += args.A
	}
	channel.Send(&StreamingReply{C: sum})
	return nil
}

func (t *StreamingArith) Echo(channel *Channel) error {
	args := new(StreamingArgs)
	for {
		err := channel.Receive(args)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		channel.Send(&StreamingReply{C: args.A, Index: args.Count})
	}
	return nil
}

func TestStreamingOutput(t *testing.T) {
	client := NewPeer()
	if err := client.Bind("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := NewPeer()
	if err := server.Connect("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	server.Register(new(StreamingArith))
	go server.Serve()

	args := &StreamingArgs{3, 5, -1}
	replyChan := make(chan *StreamingReply, 10)
	call, _ := client.Open("StreamingArith.Thrive", args, replyChan)

	count := 0
	for reply := range replyChan {
		if reply.Index != count {
			t.Fatal("unexpected value:", reply.Index)
		}
		count += 1
	}

	if call.Error != nil {
		t.Fatal("unexpected error:", call.Error.Error())
	}

	if count != 5 {
		t.Fatal("Didn't receive the right number of packets back:", count)
	}

}

func TestStreamingInput(t *testing.T) {
	client := NewPeer()
	if err := client.Bind("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := NewPeer()
	if err := server.Connect("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	server.Register(new(StreamingArith))
	go server.Serve()

	input := new(SendStream)
	reply := new(StreamingReply)
	call, err := client.Open("StreamingArith.Sum", input, reply)
	if err != nil {
		t.Fatal(err.Error())
	}

	input.Send(&StreamingArgs{9, 0, 0})
	input.Send(&StreamingArgs{3, 0, 0})
	input.Send(&StreamingArgs{3, 0, 0})
	input.Send(&StreamingArgs{6, 0, 0})
	input.SendLast(&StreamingArgs{9, 0, 0})

	<-call.Done

	if call.Error != nil {
		t.Fatal("unexpected error:", call.Error.Error())
	}

	if reply.C != 30 {
		t.Fatal("Didn't receive the right sum value back:", reply.C)
	}

}

func TestStreamingInputOutput(t *testing.T) {
	client := NewPeer()
	if err := client.Bind("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := NewPeer()
	if err := server.Connect("127.0.0.1:9876"); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	server.Register(new(StreamingArith))
	go server.Serve()

	input := new(SendStream)
	output := make(chan *StreamingReply, 10)
	call, err := client.Open("StreamingArith.Echo", input, output)
	if err != nil {
		t.Fatal(err.Error())
	}

	count := 0
	go func() {
		for reply := range output {
			count += reply.Index
		}
	}()

	input.Send(&StreamingArgs{1, 1, 0})
	input.Send(&StreamingArgs{2, 1, 0})
	time.Sleep(1 * time.Second)
	input.Send(&StreamingArgs{3, 1, 0})
	input.Send(&StreamingArgs{4, 1, 0})

	if count < 2 {
		t.Fatal("4 messages have been sent but only", count, "have been recieved")
	}
	input.SendLast(&StreamingArgs{5, 1, 0})

	<-call.Done

	if call.Error != nil {
		t.Fatal("unexpected error:", call.Error.Error())
	}

	if count != 5 {
		t.Fatal("Didn't receive the right number of values back:", count)
	}

}
