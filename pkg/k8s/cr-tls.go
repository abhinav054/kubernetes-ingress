// Copyright 2026 HAProxy Technologies
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package k8s

import (
	"k8s.io/client-go/tools/cache"

	v3 "github.com/haproxytech/kubernetes-ingress/crs/api/ingress/v3"
	informers "github.com/haproxytech/kubernetes-ingress/crs/generated/api/ingress/v3/informers/externalversions"
	k8ssync "github.com/haproxytech/kubernetes-ingress/pkg/k8s/sync"
	"github.com/haproxytech/kubernetes-ingress/pkg/store"
	"github.com/haproxytech/kubernetes-ingress/pkg/utils"
)

type TLSCR struct{}

func NewTLSCRV3() TLSCR {
	return TLSCR{}
}

func (c TLSCR) GetKind() string {
	return "TLS"
}

func (c TLSCR) GetInformerV3(eventChan chan k8ssync.SyncDataEvent, factory informers.SharedInformerFactory, osArgs utils.OSArgs) cache.SharedIndexInformer { //nolint:ireturn
	informer := factory.Ingress().V3().TLS().Informer()

	sendToChannel := func(eventChan chan k8ssync.SyncDataEvent, object interface{}, status store.Status) {
		data, ok := object.(*v3.TLS)
		if !ok {
			logger.Warning(CRSGroupVersionV3 + ": type mismatch with TLS kind")
			return
		}
		dataName := data.GetName()
		dataNS := data.GetNamespace()
		logger.Debugf("%s %s: %s", dataNS, status, dataName)
		if status == store.DELETED {
			data = nil
			logger.Infof("TLS CR '%s/%s' deleted; certificate references will be reconciled", dataNS, dataName)
		}
		eventChan <- k8ssync.SyncDataEvent{
			SyncType:  k8ssync.SyncType(c.GetKind()),
			Namespace: dataNS, Name: dataName, Data: data,
		}
	}

	errW := informer.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		go logger.Debug("TLS CR informer error: %s", err)
	})
	logger.Error(errW)
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			sendToChannel(eventChan, obj, store.ADDED)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			sendToChannel(eventChan, newObj, store.MODIFIED)
		},
		DeleteFunc: func(obj interface{}) {
			sendToChannel(eventChan, obj, store.DELETED)
		},
	})
	logger.Error(err)
	return informer
}
