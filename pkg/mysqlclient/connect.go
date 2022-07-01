// Copyright (c) 2022, Oracle and/or its affiliates.
//
// Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/

package mysqlclient

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"
)

const (
	mysqldPort          = 3306
	ndbOperatorUser     = "ndb-operator-user"
	ndbOperatorPassword = "Operator@123"
	sqlDriverName       = "mysql"
)

// Connect opens a connection to the first MySQL Server pod managed by the given MySQL Server StatefulSet
func Connect(mysqldSfset *appsv1.StatefulSet, dbName string) (*sql.DB, error) {

	// Generate the MySQL Server host using the hostname of StatefulSet's pod-0
	mysqldHost := fmt.Sprintf("%s-0.%s.%s", mysqldSfset.Name, mysqldSfset.Spec.ServiceName, mysqldSfset.Namespace)

	// Generate the complete address to connect to
	dataSource := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=10s",
		ndbOperatorUser, ndbOperatorPassword, mysqldHost, mysqldPort, dbName)
	db, err := sql.Open(sqlDriverName, dataSource)
	if err != nil {
		klog.Infof("Error opening connection to MySQL server at %q : %s", mysqldHost, err)
		return nil, err
	}

	// Verify the DB is connected
	if db != nil {
		err = db.Ping()
	}

	if db == nil || err != nil {
		klog.Infof("Error connecting to the MySQL server at %q : %s", mysqldHost, err)
		return nil, err
	}

	// Recommended settings
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	return db, nil
}