// Package mbserver implments a Modbus server (slave).
package mbserver

import (
	"io"
	"net"
	"sync"

	"github.com/goburrow/serial"
)

// Server is a Modbus slave with allocated memory for discrete inputs, coils, etc.
type Server struct {
	// Debug enables more verbose messaging.
	Debug            bool
	slaveId          uint8
	listeners        []net.Listener
	ports            []serial.Port
	portsWG          sync.WaitGroup
	portsCloseChan   chan struct{}
	requestChan      chan *Request
	function         [256](func(*Server, Framer) ([]byte, *Exception))
	DiscreteInputs   []byte
	Coils            []byte
	HoldingRegisters []uint16
	InputRegisters   []uint16
}

// Request contains the connection and Modbus frame.
type Request struct {
	conn  io.ReadWriteCloser
	frame Framer
}

// NewServerWithSlaveId creates a new Modbus server (slave).
func NewServerWithSlaveId(slaveId uint8) *Server {
	s := &Server{
		slaveId: slaveId,
	}

	// Allocate Modbus memory maps.
	s.DiscreteInputs = make([]byte, MaxRegisterSize)
	s.Coils = make([]byte, MaxRegisterSize)
	s.HoldingRegisters = make([]uint16, MaxRegisterSize)
	s.InputRegisters = make([]uint16, MaxRegisterSize)

	// Add default functions.
	s.function[ReadCoilsFC] = ReadCoils
	s.function[ReadDiscreteInputsFC] = ReadDiscreteInputs
	s.function[ReadHoldingRegistersFC] = ReadHoldingRegisters
	s.function[ReadInputRegistersFC] = ReadInputRegisters
	s.function[WriteSingleCoilFC] = WriteSingleCoil
	s.function[WriteHoldingRegisterFC] = WriteHoldingRegister
	s.function[WriteMultipleCoilsFC] = WriteMultipleCoils
	s.function[WriteHoldingRegistersFC] = WriteHoldingRegisters

	s.requestChan = make(chan *Request)
	s.portsCloseChan = make(chan struct{})

	go s.handler()

	return s
}

// NewServer creates a new Modbus server (slave). default slaveId 1
func NewServer() *Server {
	return NewServerWithSlaveId(1)
}

// RegisterFunctionHandler override the default behavior for a given Modbus function.
func (s *Server) RegisterFunctionHandler(funcCode uint8, function func(*Server, Framer) ([]byte, *Exception)) {
	s.function[funcCode] = function
}

func (s *Server) handle(request *Request) Framer {
	var exception *Exception
	var data []byte

	response := request.frame.Copy()

	function := request.frame.GetFunction()
	if s.function[function] != nil {
		data, exception = s.function[function](s, request.frame)
		response.SetData(data)
	} else {
		exception = &IllegalFunction
	}

	if exception != &Success {
		response.SetException(exception)
	}

	return response
}

// All requests are handled synchronously to prevent modbus memory corruption.
func (s *Server) handler() {
	for {
		request := <-s.requestChan
		if request.frame.GetSlaveId() != s.slaveId {
			continue
		}
		response := s.handle(request)
		request.conn.Write(response.Bytes())
	}
}

// Close stops listening to TCP/IP ports and closes serial ports.
func (s *Server) Close() {
	for _, listen := range s.listeners {
		listen.Close()
	}

	close(s.portsCloseChan)
	s.portsWG.Wait()

	for _, port := range s.ports {
		port.Close()
	}
}
