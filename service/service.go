package service

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type Service struct {
	Name   string
	Typ    reflect.Type
	Rcvr   reflect.Value // 结构体实例的本身
	Method map[string]*MethodType
}

type MethodType struct {
	Method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

func (m *MethodType) NumCalls() uint64 {
	// 使用 sync/atomic 包的 LoadUint64 函数原子地加载 m.numCalls 的值，确保在多线程环境下读取该值的安全性
	return atomic.LoadUint64(&m.numCalls)
}

func (m *MethodType) NewArgv() reflect.Value {
	var argv reflect.Value
	// arg may be a pointer type, or a value type
	// 指针类型和值类型创建实例的方式有细微区别
	if m.ArgType.Kind() == reflect.Ptr {
		// 创建一个指向该类型的指针
		argv = reflect.New(m.ArgType.Elem())
	} else {
		// 创建一个该类型的值
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *MethodType) NewReplyv() reflect.Value {
	// reply must be a pointer type
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

func NewService(rcvr interface{}) *Service {
	s := new(Service)
	s.Rcvr = reflect.ValueOf(rcvr)

	// 如果是指针类型，它会返回空指针；如果不是指针类型，它会直接返回 s.Rcvr 本身
	//s.Name = s.Rcvr.Type().Name()

	// 如果是指针类型，它会返回指针所指向的值的反射值；如果不是指针类型，它会直接返回 s.Rcvr 本身
	s.Name = reflect.Indirect(s.Rcvr).Type().Name()

	s.Typ = reflect.TypeOf(rcvr)
	// 检查服务名称是否为导出的标识符,如果以大写字母开头，那么它就是导出的，可以被其他包访问
	if !ast.IsExported(s.Name) {
		log.Fatalf("rpc server: %s is not a valid Service Name", s.Name)
	}
	s.registerMethods()
	return s
}

func (s *Service) registerMethods() {
	s.Method = make(map[string]*MethodType)
	for i := 0; i < s.Typ.NumMethod(); i++ {
		method := s.Typ.Method(i)
		//mType := Method.Type
		// 筛选符合特定参数和返回值数量要求的方法
		if method.Type.NumIn() != 3 || method.Type.NumOut() != 1 {
			continue
		}

		// 检查返回值类型是否为 error
		if method.Type.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := method.Type.In(1), method.Type.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		s.Method[method.Name] = &MethodType{
			Method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.Name, method.Name)
	}
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// 实现 Call 方法，即能够通过反射值调用方法
func (s *Service) Call(m *MethodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	f := m.Method.Func
	returnValues := f.Call([]reflect.Value{s.Rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
